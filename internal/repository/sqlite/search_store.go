package sqlite

import (
	"context"
	"strings"

	"sessionflow/internal/domain"
)

// Marcadores do trecho destacado: caracteres de uso privado (U+E000 e
// U+E001), que não aparecem em texto comum. O serviço os converte em
// segmentos; a interface nunca recebe HTML.
const (
	MarkOpen  = ""
	MarkClose = ""
)

// SearchFilter seleciona mensagens pelo texto. Match já está na sintaxe
// FTS5 e deve vir sanitizado (service.BuildMatch).
type SearchFilter struct {
	Match         string
	Providers     []domain.ProviderID // vazio = todos
	ProjectPath   string              // "" = todos
	IncludeHidden bool                // subagentes e sessões automáticas
	Limit, Offset int
	CountCap      int // total contado até este limite (> 0)
}

// SearchHit é uma mensagem encontrada e a sessão dela.
type SearchHit struct {
	Session   domain.Session
	Seq       int
	Role      domain.Role
	Timestamp int64  // ms; 0 = desconhecido
	Snippet   string // trecho com MarkOpen/MarkClose em volta dos termos
}

// SearchMessages devolve uma página de mensagens por relevância (bm25; em
// empate, a mais recente) e o total, contado até CountCap.
func (s *IndexStore) SearchMessages(ctx context.Context, f SearchFilter) ([]SearchHit, int, error) {
	where := []string{"messages_fts MATCH ?"}
	args := []any{f.Match}
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
	if !f.IncludeHidden {
		where = append(where, visibleS)
	}
	from := ` FROM messages_fts
		JOIN messages m ON m.id = messages_fts.rowid
		JOIN sessions s ON s.id = m.session_id
		LEFT JOIN projects p ON p.id = s.project_id
		WHERE ` + strings.Join(where, " AND ")

	var total int
	if err := s.db.QueryRowContext(ctx,
		"SELECT count(*) FROM (SELECT 1"+from+" LIMIT ?)", append(args, f.CountCap)...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return nil, 0, nil
	}

	q := "SELECT " + sessionCols + `, m.seq, m.role, coalesce(m.ts, 0),
		snippet(messages_fts, 0, '` + MarkOpen + `', '` + MarkClose + `', '…', 24)` + from +
		" ORDER BY messages_fts.rank, m.ts IS NULL, m.ts DESC, m.id LIMIT ? OFFSET ?"
	rows, err := s.db.QueryContext(ctx, q, append(args, f.Limit, f.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []SearchHit
	for rows.Next() {
		var h SearchHit
		sess, err := scanSession(withExtra{rows, []any{&h.Seq, &h.Role, &h.Timestamp, &h.Snippet}})
		if err != nil {
			return nil, 0, err
		}
		h.Session = sess
		out = append(out, h)
	}
	return out, total, rows.Err()
}

// withExtra lê colunas além das de sessionCols com o mesmo scanSession.
type withExtra struct {
	r     scanner
	extra []any
}

func (w withExtra) Scan(dest ...any) error { return w.r.Scan(append(dest, w.extra...)...) }
