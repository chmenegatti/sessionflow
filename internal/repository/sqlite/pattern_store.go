package sqlite

import (
	"context"
	"database/sql"
	"math"
	"time"

	"sessionflow/internal/domain"
)

// Padrões de atividade (docs/ANALYTICS.md §Padrões). Mesma regra do
// Dashboard: só sessões principais e não automáticas, mensagens com texto.

// SessionFact descreve uma sessão visível com mensagem com texto no período.
type SessionFact struct {
	SessionID        int64
	Provider         domain.ProviderID
	Project          string
	ProjectName      string
	CreatedAt        *int64 // ms
	FirstTs, LastTs  *int64 // primeira e última mensagem com horário (todo o histórico da sessão)
	MessagesInPeriod int    // mensagens com texto no período
	ResumedAt        *int64 // 1ª mensagem com texto no período após pausa ≥ gap desde a anterior com texto
}

func msRange(f StatsFilter) (from, to int64) {
	from, to = math.MinInt64, math.MaxInt64
	if !f.From.IsZero() {
		from = f.From.UnixMilli()
	}
	if !f.To.IsZero() {
		to = f.To.UnixMilli()
	}
	return from, to
}

// SessionFacts devolve os fatos das sessões com atividade no recorte. A
// mensagem anterior que mede a pausa pode estar antes do período.
func (s *IndexStore) SessionFacts(ctx context.Context, f StatsFilter, gap time.Duration) ([]SessionFact, error) {
	where, args := f.where(true)
	from, to := msRange(f)
	q := `WITH win AS (SELECT m.session_id, count(*) AS n` + statsFrom + where + ` GROUP BY m.session_id),
	gaps AS (
	  SELECT m.session_id, m.ts, m.ts - lag(m.ts) OVER (PARTITION BY m.session_id ORDER BY m.ts, m.seq) AS gap
	  FROM messages m WHERE m.session_id IN (SELECT session_id FROM win) AND m.has_text = 1 AND m.ts IS NOT NULL)
	SELECT s.id, s.provider, coalesce(p.path, ''), coalesce(p.name, ''), s.created_at,
	  (SELECT min(ts) FROM messages WHERE session_id = s.id), (SELECT max(ts) FROM messages WHERE session_id = s.id),
	  win.n,
	  (SELECT min(g.ts) FROM gaps g WHERE g.session_id = s.id AND g.gap >= ? AND g.ts >= ? AND g.ts < ?)
	FROM win JOIN sessions s ON s.id = win.session_id LEFT JOIN projects p ON p.id = s.project_id
	ORDER BY s.id`
	rows, err := s.db.QueryContext(ctx, q, append(args, gap.Milliseconds(), from, to)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SessionFact
	for rows.Next() {
		var x SessionFact
		var created, first, last, resumed sql.NullInt64
		if err := rows.Scan(&x.SessionID, &x.Provider, &x.Project, &x.ProjectName, &created, &first, &last, &x.MessagesInPeriod, &resumed); err != nil {
			return nil, err
		}
		x.CreatedAt, x.FirstTs, x.LastTs, x.ResumedAt = int64Ptr(created), int64Ptr(first), int64Ptr(last), int64Ptr(resumed)
		out = append(out, x)
	}
	return out, rows.Err()
}

// SessionStarts devolve o início (created_at, ms) das sessões visíveis
// criadas no recorte, em ordem.
func (s *IndexStore) SessionStarts(ctx context.Context, f StatsFilter) ([]int64, error) {
	from, to := msRange(f)
	q := "SELECT s.created_at FROM sessions s WHERE " + visibleS + " AND s.created_at >= ? AND s.created_at < ?"
	args := []any{from, to}
	if c, a := providersIn(f.Providers); c != "" {
		q += " AND " + c
		args = append(args, a...)
	}
	if f.Project != "" {
		q += " AND s.project_id = (SELECT id FROM projects WHERE path = ?)"
		args = append(args, f.Project)
	}
	rows, err := s.db.QueryContext(ctx, q+" ORDER BY s.created_at", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var v int64
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// DayActivity é a atividade de um dia local por projeto e provider.
type DayActivity struct {
	Day         string // AAAA-MM-DD local
	Project     string
	ProjectName string
	Provider    domain.ProviderID
	Sessions    int // sessões com mensagem com texto no dia
	Messages    int
}

// DailyActivity agrupa as mensagens com texto do recorte por dia local,
// projeto e provider. Com f.Zones, cada mensagem usa o deslocamento do fuso
// no próprio instante (dia civil certo dos dois lados do horário de verão).
func (s *IndexStore) DailyActivity(ctx context.Context, f StatsFilter) ([]DayActivity, error) {
	where, args := f.where(true)
	off, oargs := f.offsetExpr()
	q := "SELECT (m.ts / 1000 + " + off + ") / 86400 AS d, coalesce(p.path, ''), coalesce(p.name, ''), s.provider, count(DISTINCT m.session_id), count(*)" +
		statsFrom + " LEFT JOIN projects p ON p.id = s.project_id" + where + " GROUP BY d, 2, 4 ORDER BY d"
	rows, err := s.db.QueryContext(ctx, q, append(oargs, args...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DayActivity
	for rows.Next() {
		var r DayActivity
		var d int64
		if err := rows.Scan(&d, &r.Project, &r.ProjectName, &r.Provider, &r.Sessions, &r.Messages); err != nil {
			return nil, err
		}
		r.Day = bucketKey(BucketDay, d)
		out = append(out, r)
	}
	return out, rows.Err()
}

// LastActivityByProject devolve, por projeto, a última mensagem com texto
// (ms) de sessões visíveis antes de before.
func (s *IndexStore) LastActivityByProject(ctx context.Context, providers []domain.ProviderID, before time.Time) (map[string]int64, error) {
	where, args := StatsFilter{To: before, Providers: providers}.where(true)
	rows, err := s.db.QueryContext(ctx, "SELECT p.path, max(m.ts)"+statsFrom+" JOIN projects p ON p.id = s.project_id"+where+" GROUP BY p.path", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var p string
		var ts int64
		if err := rows.Scan(&p, &ts); err != nil {
			return nil, err
		}
		out[p] = ts
	}
	return out, rows.Err()
}
