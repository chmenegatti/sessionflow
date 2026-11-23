package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"sessionflow/internal/domain"
)

// ErrNotFound indica sessão ausente no índice.
var ErrNotFound = errors.New("sessão não encontrada")

// SessionFilter seleciona sessões do índice.
type SessionFilter struct {
	Providers     []domain.ProviderID // vazio = todos
	ProjectPath   string              // "" = todos
	Keys          []string            // "provider:id"; nil = sem restrição, vazio = nenhum resultado
	IncludeHidden bool                // inclui subagentes e sessões automáticas
	Oldest        bool                // ordem crescente de atividade
	UpdatedSince  time.Time           // só sessões com atividade a partir daqui (zero = todas)
	Limit, Offset int
}

// visibleS é a regra de visible com o alias "s".
const visibleS = "s.parent_native_id IS NULL AND s.automated = 0"

const sessionCols = `s.provider, s.native_id, coalesce(s.parent_native_id, ''), s.title, s.first_prompt, s.model,
	coalesce(s.created_at, 0), coalesce(s.updated_at, 0), s.message_count, s.archived, s.automated,
	s.source_kind, s.source_path, coalesce(p.path, '')`

// ListSessions devolve uma página de sessões e o total do filtro, por
// atividade (updated_at) decrescente, ou crescente com Oldest.
func (s *IndexStore) ListSessions(ctx context.Context, f SessionFilter) ([]domain.Session, int, error) {
	if f.Keys != nil && len(f.Keys) == 0 {
		return nil, 0, nil
	}
	var where []string
	var args []any
	if len(f.Providers) > 0 {
		marks := make([]string, len(f.Providers))
		for i, p := range f.Providers {
			marks[i] = "?"
			args = append(args, p)
		}
		where = append(where, "s.provider IN ("+strings.Join(marks, ",")+")")
	}
	if f.ProjectPath != "" {
		where = append(where, "p.path = ?")
		args = append(args, f.ProjectPath)
	}
	if f.Keys != nil {
		keys, _ := json.Marshal(f.Keys)
		where = append(where, "s.provider || ':' || s.native_id IN (SELECT value FROM json_each(?))")
		args = append(args, string(keys))
	}
	if !f.IncludeHidden {
		where = append(where, visibleS)
	}
	if !f.UpdatedSince.IsZero() {
		where = append(where, "s.updated_at >= ?")
		args = append(args, f.UpdatedSince.UnixMilli())
	}
	cond := ""
	if len(where) > 0 {
		cond = " WHERE " + strings.Join(where, " AND ")
	}
	from := " FROM sessions s LEFT JOIN projects p ON p.id = s.project_id" + cond

	var total int
	if err := s.db.QueryRowContext(ctx, "SELECT count(*)"+from, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	dir := "DESC"
	if f.Oldest {
		dir = "ASC"
	}
	q := "SELECT " + sessionCols + from +
		" ORDER BY s.updated_at IS NULL, s.updated_at " + dir + ", s.id " + dir + " LIMIT ? OFFSET ?"
	rows, err := s.db.QueryContext(ctx, q, append(args, f.Limit, f.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []domain.Session
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, sess)
	}
	return out, total, rows.Err()
}

// GetSession devolve uma sessão pela chave.
func (s *IndexStore) GetSession(ctx context.Context, key domain.SessionKey) (domain.Session, error) {
	row := s.db.QueryRowContext(ctx, "SELECT "+sessionCols+
		" FROM sessions s LEFT JOIN projects p ON p.id = s.project_id WHERE s.provider = ? AND s.native_id = ?",
		key.Provider, key.NativeID)
	sess, err := scanSession(row)
	if errors.Is(err, sql.ErrNoRows) {
		return sess, ErrNotFound
	}
	return sess, err
}

// ExistingKeys informa quais chaves ("provider:id") existem no índice.
func (s *IndexStore) ExistingKeys(ctx context.Context, keys []string) (map[string]bool, error) {
	out := map[string]bool{}
	if len(keys) == 0 {
		return out, nil
	}
	b, _ := json.Marshal(keys)
	rows, err := s.db.QueryContext(ctx, `
		SELECT provider || ':' || native_id FROM sessions
		WHERE provider || ':' || native_id IN (SELECT value FROM json_each(?))`, string(b))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out[k] = true
	}
	return out, rows.Err()
}

// CountChildren conta subagentes/subsessões de uma sessão.
func (s *IndexStore) CountChildren(ctx context.Context, key domain.SessionKey) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		"SELECT count(*) FROM sessions WHERE provider = ? AND parent_native_id = ?", key.Provider, key.NativeID).Scan(&n)
	return n, err
}

type scanner interface{ Scan(dest ...any) error }

func scanSession(r scanner) (domain.Session, error) {
	var (
		s                domain.Session
		parent           string
		created, updated int64
		kind             string
	)
	err := r.Scan(&s.Key.Provider, &s.Key.NativeID, &parent, &s.Title, &s.FirstPrompt, &s.Model,
		&created, &updated, &s.MessageCount, &s.Archived, &s.Automated, &kind, &s.SourcePath, &s.ProjectPath)
	if err != nil {
		return s, err
	}
	s.SourceKind = domain.SourceKind(kind)
	s.CreatedAt, s.UpdatedAt = fromMillis(created), fromMillis(updated)
	if parent != "" {
		s.Parent = &domain.SessionKey{Provider: s.Key.Provider, NativeID: parent}
	}
	return s, nil
}

func fromMillis(ms int64) time.Time {
	if ms == 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms).UTC()
}

// ListProjects devolve os projetos com sessões visíveis, do mais ativo
// para o menos ativo.
func (s *IndexStore) ListProjects(ctx context.Context) ([]domain.Project, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.path, p.name, group_concat(DISTINCT s.provider), count(*), coalesce(max(s.updated_at), 0)
		FROM projects p JOIN sessions s ON s.project_id = p.id
		WHERE `+visibleS+`
		GROUP BY p.id
		ORDER BY max(s.updated_at) DESC, p.path`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Project
	for rows.Next() {
		var p domain.Project
		var providers string
		var last int64
		if err := rows.Scan(&p.Path, &p.Name, &providers, &p.SessionCount, &last); err != nil {
			return nil, err
		}
		for _, id := range domain.AllProviders() { // ordem estável
			if strings.Contains(","+providers+",", ","+string(id)+",") {
				p.Providers = append(p.Providers, id)
			}
		}
		p.LastActivity = fromMillis(last)
		out = append(out, p)
	}
	return out, rows.Err()
}
