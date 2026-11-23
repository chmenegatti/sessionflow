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

// IndexStore grava e consulta o índice (index.db).
type IndexStore struct{ db *sql.DB }

// NewIndexStore cria o store sobre um banco aberto por OpenIndex.
func NewIndexStore(db *sql.DB) *IndexStore { return &IndexStore{db: db} }

// SyncState é o estado de sincronização de uma fonte.
type SyncState struct {
	Size    int64
	MTimeNS int64
	Cursor  string // opaco; hoje, o Fingerprint da fonte
	Status  string // ok | partial | error | unsupported
	Error   string // nunca contém conteúdo de conversa
}

// SyncStates devolve o estado das fontes de um provider, por caminho.
func (s *IndexStore) SyncStates(ctx context.Context, p domain.ProviderID) (map[string]SyncState, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT source_path, size, mtime_ns, cursor, status, coalesce(error, '') FROM sync_state WHERE provider = ?", p)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]SyncState{}
	for rows.Next() {
		var path string
		var st SyncState
		if err := rows.Scan(&path, &st.Size, &st.MTimeNS, &st.Cursor, &st.Status, &st.Error); err != nil {
			return nil, err
		}
		out[path] = st
	}
	return out, rows.Err()
}

// SetSyncState grava o estado de uma fonte sem mexer nas sessões dela
// (usado quando o parse falha: as sessões da última leitura boa ficam).
func (s *IndexStore) SetSyncState(ctx context.Context, p domain.ProviderID, path string, st SyncState) error {
	return putSyncState(ctx, s.db, p, path, st)
}

type execer interface {
	ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error)
}

func putSyncState(ctx context.Context, x execer, p domain.ProviderID, path string, st SyncState) error {
	var errText any
	if st.Error != "" {
		errText = st.Error
	}
	_, err := x.ExecContext(ctx, `
		INSERT INTO sync_state (provider, source_path, size, mtime_ns, cursor, status, error, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (provider, source_path) DO UPDATE SET
		  size = excluded.size, mtime_ns = excluded.mtime_ns, cursor = excluded.cursor,
		  status = excluded.status, error = excluded.error, updated_at = excluded.updated_at`,
		p, path, st.Size, st.MTimeNS, st.Cursor, st.Status, errText, time.Now().UnixMilli())
	return err
}

// RemoveSourcesExcept apaga sessões e estado das fontes de um provider que
// não estão em keep (arquivo apagado ou banco removido: o índice espelha a
// fonte). Projetos sem sessões também saem.
func (s *IndexStore) RemoveSourcesExcept(ctx context.Context, p domain.ProviderID, keep []string) (removed int, err error) {
	keepSet := make(map[string]bool, len(keep))
	for _, k := range keep {
		keepSet[k] = true
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT source_path FROM sync_state WHERE provider = ?
		UNION SELECT source_path FROM sessions WHERE provider = ?`, p, p)
	if err != nil {
		return 0, err
	}
	var gone []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			rows.Close()
			return 0, err
		}
		if !keepSet[path] {
			gone = append(gone, path)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(gone) == 0 {
		return 0, nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	for _, path := range gone {
		res, err := tx.ExecContext(ctx, "DELETE FROM sessions WHERE provider = ? AND source_path = ?", p, path)
		if err != nil {
			return 0, err
		}
		n, _ := res.RowsAffected()
		removed += int(n)
		if _, err := tx.ExecContext(ctx, "DELETE FROM sync_state WHERE provider = ? AND source_path = ?", p, path); err != nil {
			return 0, err
		}
	}
	if err := deleteOrphanProjects(ctx, tx); err != nil {
		return 0, err
	}
	return removed, tx.Commit()
}

func deleteOrphanProjects(ctx context.Context, x execer) error {
	_, err := x.ExecContext(ctx,
		"DELETE FROM projects WHERE id NOT IN (SELECT project_id FROM sessions WHERE project_id IS NOT NULL)")
	return err
}

// CountSessions devolve o número de sessões por provider.
func (s *IndexStore) CountSessions(ctx context.Context) (map[domain.ProviderID]int, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT provider, count(*) FROM sessions GROUP BY provider")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[domain.ProviderID]int{}
	for rows.Next() {
		var p domain.ProviderID
		var n int
		if err := rows.Scan(&p, &n); err != nil {
			return nil, err
		}
		out[p] = n
	}
	return out, rows.Err()
}

// visible: sessões principais com prompt humano. Subagentes/subsessões e
// sessões automáticas (SDK, hooks) ficam ocultos por padrão
// (docs/discovery/00-relatorio.md §4, regras 8 e 9).
const visible = "parent_native_id IS NULL AND automated = 0"

// SessionCount separa sessões visíveis das ocultas.
type SessionCount struct {
	Visible int
	Hidden  int
}

// SessionCounts devolve as contagens por provider.
func (s *IndexStore) SessionCounts(ctx context.Context) (map[domain.ProviderID]SessionCount, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT provider, coalesce(sum("+visible+"), 0), coalesce(sum(NOT ("+visible+")), 0) FROM sessions GROUP BY provider")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[domain.ProviderID]SessionCount{}
	for rows.Next() {
		var p domain.ProviderID
		var c SessionCount
		if err := rows.Scan(&p, &c.Visible, &c.Hidden); err != nil {
			return nil, err
		}
		out[p] = c
	}
	return out, rows.Err()
}

// VisibleProjects conta projetos com ao menos uma sessão visível.
func (s *IndexStore) VisibleProjects(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		"SELECT count(DISTINCT project_id) FROM sessions WHERE project_id IS NOT NULL AND "+visible).Scan(&n)
	return n, err
}

// IndexedMessage é a parte de uma mensagem que vai para o índice.
type IndexedMessage struct {
	Seq        int
	NativeID   string
	Role       domain.Role
	Timestamp  *time.Time
	SearchText string
	HasText    bool     // conta como mensagem (domain.CountsAsMessage)
	Tools      []string // ferramentas chamadas, em ordem
}

// SourceTx substitui, numa transação, tudo o que veio de uma fonte: as
// sessões antigas dela são apagadas (cascade em messages e no FTS) e as
// novas, inseridas. Commit grava também o SyncState.
type SourceTx struct {
	tx       *sql.Tx
	provider domain.ProviderID
	path     string
	kind     domain.SourceKind
	ids      map[string]int64 // native_id → sessions.id (0 = duplicada)
	insMsg   *sql.Stmt

	// Duplicates conta sessões ignoradas porque o mesmo ID já existe no
	// índice vindo de outra fonte.
	Duplicates int
}

// BeginSource abre a transação de uma fonte.
func (s *IndexStore) BeginSource(ctx context.Context, p domain.ProviderID, path string, kind domain.SourceKind) (*SourceTx, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM sessions WHERE provider = ? AND source_path = ?", p, path); err != nil {
		tx.Rollback()
		return nil, err
	}
	ins, err := tx.PrepareContext(ctx,
		"INSERT INTO messages (session_id, seq, native_id, role, ts, search_text, has_text, tools) VALUES (?, ?, ?, ?, ?, ?, ?, ?)")
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	return &SourceTx{tx: tx, provider: p, path: path, kind: kind, ids: map[string]int64{}, insMsg: ins}, nil
}

// sessionID garante a linha da sessão e devolve seu id (0 se duplicada).
func (t *SourceTx) sessionID(ctx context.Context, key domain.SessionKey) (int64, error) {
	if id, ok := t.ids[key.NativeID]; ok {
		return id, nil
	}
	var id int64
	err := t.tx.QueryRowContext(ctx, `
		INSERT INTO sessions (provider, native_id, source_kind, source_path)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (provider, native_id) DO NOTHING
		RETURNING id`, key.Provider, key.NativeID, t.kind, t.path).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		t.Duplicates++
		err = nil
	}
	if err != nil {
		return 0, err
	}
	t.ids[key.NativeID] = id
	return id, nil
}

// AddMessage grava uma mensagem da sessão key.
func (t *SourceTx) AddMessage(ctx context.Context, key domain.SessionKey, m IndexedMessage) error {
	id, err := t.sessionID(ctx, key)
	if err != nil || id == 0 {
		return err
	}
	tools := []byte("[]")
	if len(m.Tools) > 0 {
		if tools, err = json.Marshal(m.Tools); err != nil {
			return err
		}
	}
	_, err = t.insMsg.ExecContext(ctx, id, m.Seq, nullString(m.NativeID), m.Role, nullMillis(m.Timestamp), m.SearchText, m.HasText, string(tools))
	return err
}

// AddUsage grava um registro de consumo da sessão key. O mesmo ResponseID na
// mesma sessão é ignorado (o adaptador já deduplica; isto garante).
func (t *SourceTx) AddUsage(ctx context.Context, key domain.SessionKey, u domain.Usage) error {
	id, err := t.sessionID(ctx, key)
	if err != nil || id == 0 {
		return err
	}
	_, err = t.tx.ExecContext(ctx, `
		INSERT INTO usage (session_id, response_id, seq, ts, model, input_tokens, output_tokens,
		  cache_read_tokens, cache_creation_tokens, reasoning_tokens, total_tokens, derived, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (session_id, response_id) DO NOTHING`,
		id, u.ResponseID, u.Seq, nullMillis(u.Timestamp), u.Model,
		nullInt(u.Input), nullInt(u.Output), nullInt(u.CacheRead), nullInt(u.CacheCreation),
		nullInt(u.Reasoning), nullInt(u.Total), strings.Join(u.Derived, ","), string(u.Status))
	return err
}

func nullInt(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}

// PutSession grava os metadados finais da sessão.
func (t *SourceTx) PutSession(ctx context.Context, s domain.Session) error {
	id, err := t.sessionID(ctx, s.Key)
	if err != nil || id == 0 {
		return err
	}
	var projectID any
	if s.ProjectPath != "" {
		var pid int64
		err := t.tx.QueryRowContext(ctx, `
			INSERT INTO projects (path, name) VALUES (?, ?)
			ON CONFLICT (path) DO UPDATE SET name = excluded.name
			RETURNING id`, s.ProjectPath, domain.ProjectName(s.ProjectPath)).Scan(&pid)
		if err != nil {
			return err
		}
		projectID = pid
	}
	var parent any
	if s.Parent != nil {
		parent = s.Parent.NativeID
	}
	_, err = t.tx.ExecContext(ctx, `
		UPDATE sessions SET parent_native_id = ?, project_id = ?, title = ?, first_prompt = ?, model = ?,
		  created_at = ?, updated_at = ?, message_count = ?, archived = ?, automated = ?
		WHERE id = ?`,
		parent, projectID, s.Title, s.FirstPrompt, s.Model,
		nullMillis(timePtr(s.CreatedAt)), nullMillis(timePtr(s.UpdatedAt)), s.MessageCount,
		s.Archived, s.Automated, id)
	return err
}

// Commit grava o estado da fonte, remove projetos órfãos e confirma.
func (t *SourceTx) Commit(ctx context.Context, st SyncState) error {
	defer t.insMsg.Close()
	if err := putSyncState(ctx, t.tx, t.provider, t.path, st); err != nil {
		t.tx.Rollback()
		return err
	}
	if err := deleteOrphanProjects(ctx, t.tx); err != nil {
		t.tx.Rollback()
		return err
	}
	return t.tx.Commit()
}

// Rollback descarta a transação (a leitura anterior da fonte permanece).
func (t *SourceTx) Rollback() {
	t.insMsg.Close()
	t.tx.Rollback()
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullMillis(t *time.Time) any {
	if t == nil || t.IsZero() {
		return nil
	}
	return t.UnixMilli()
}

func timePtr(t time.Time) *time.Time { return &t }
