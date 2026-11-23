package sqlite

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"sessionflow/internal/domain"
)

// Bucket é o tamanho do intervalo das séries do Dashboard.
type Bucket string

const (
	BucketHour  Bucket = "hour" // período "hoje"
	BucketDay   Bucket = "day"
	BucketWeek  Bucket = "week" // começa na segunda-feira
	BucketMonth Bucket = "month"
)

// StatsFilter recorta as estatísticas. Mensagens sem horário nunca entram.
type StatsFilter struct {
	From, To  time.Time           // [From, To); From zero = desde o início
	Providers []domain.ProviderID // vazio = todos
	Project   string              // caminho; vazio = todos
	Bucket    Bucket
	Offset    int // segundos do fuso local (time.Now().Zone()): o dia é o local
	// Zones: o deslocamento de cada trecho do período (horário de verão).
	// Vazio = Offset para tudo. Só DailyActivity usa (dia civil por
	// mensagem); os intervalos dos gráficos seguem com Offset.
	Zones []ZoneSpan
}

// ZoneSpan: Offset (segundos) vale para horários antes de Until (ms); o
// último trecho vale até o fim.
type ZoneSpan struct {
	Until  int64
	Offset int
}

// offsetExpr: deslocamento do fuso no instante m.ts.
func (f StatsFilter) offsetExpr() (string, []any) {
	switch len(f.Zones) {
	case 0:
		return "?", []any{f.Offset}
	case 1:
		return "?", []any{f.Zones[0].Offset}
	}
	var b strings.Builder
	var args []any
	b.WriteString("CASE")
	for _, z := range f.Zones[:len(f.Zones)-1] {
		b.WriteString(" WHEN m.ts < ? THEN ?")
		args = append(args, z.Until, z.Offset)
	}
	b.WriteString(" ELSE ? END")
	return b.String(), append(args, f.Zones[len(f.Zones)-1].Offset)
}

// ActivityRow é a atividade de um provider num intervalo. Bucket é a data
// local de início do intervalo ("AAAA-MM-DD").
type ActivityRow struct {
	Bucket   string
	Provider domain.ProviderID
	Sessions int // sessões com mensagem no intervalo
	Messages int // mensagens (has_text)
}

// Totals são contagens distintas num período.
type Totals struct {
	Sessions, Messages, Projects int
}

// NamedCount é um item de ranking. Path só para projetos.
type NamedCount struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	Count int    `json:"count"`
}

// bucketExpr devolve o início do intervalo da mensagem m: para dia e semana,
// um inteiro (dias locais desde a época, uma quinta), formatado em Go; para
// mês, o texto "AAAA-MM-01". Conta inteira em vez de date() por linha: é o
// que domina o custo da consulta.
func bucketExpr(b Bucket) string {
	const day = "((m.ts / 1000 + ?) / 86400)"
	switch b {
	case BucketHour:
		return "((m.ts / 1000 + ?) / 3600)"
	case BucketWeek:
		return "(" + day + " - (" + day + " + 3) % 7)"
	case BucketMonth:
		return "strftime('%Y-%m-01', " + day + " * 86400, 'unixepoch')"
	default:
		return day
	}
}

// HourKey é o formato da chave de intervalos de uma hora.
const HourKey = "2006-01-02T15"

// bucketKey converte o valor de bucketExpr em "AAAA-MM-DD" (ou HourKey).
func bucketKey(b Bucket, v any) string {
	switch x := v.(type) {
	case int64:
		if b == BucketHour {
			return time.Unix(x*3600, 0).UTC().Format(HourKey)
		}
		return time.Unix(x*86400, 0).UTC().Format(time.DateOnly)
	case string:
		return x
	case []byte:
		return string(x)
	}
	return ""
}

// bucketArgs: quantas vezes o offset aparece em bucketExpr.
func bucketArgs(f StatsFilter) []any {
	if f.Bucket == BucketWeek {
		return []any{f.Offset, f.Offset}
	}
	return []any{f.Offset}
}

// where monta as condições comuns: sessões visíveis, providers, mensagens
// com horário no período. textOnly limita a mensagens que contam.
func (f StatsFilter) where(textOnly bool) (string, []any) {
	cond := []string{visibleS, "m.ts IS NOT NULL"}
	var args []any
	if textOnly {
		cond = append(cond, "m.has_text = 1")
	}
	if !f.From.IsZero() {
		cond = append(cond, "m.ts >= ?")
		args = append(args, f.From.UnixMilli())
	}
	if !f.To.IsZero() {
		cond = append(cond, "m.ts < ?")
		args = append(args, f.To.UnixMilli())
	}
	if c, a := providersIn(f.Providers); c != "" {
		cond = append(cond, c)
		args = append(args, a...)
	}
	if f.Project != "" {
		cond = append(cond, "s.project_id = (SELECT id FROM projects WHERE path = ?)")
		args = append(args, f.Project)
	}
	return " WHERE " + strings.Join(cond, " AND "), args
}

func providersIn(ps []domain.ProviderID) (string, []any) {
	if len(ps) == 0 {
		return "", nil
	}
	marks := make([]string, len(ps))
	args := make([]any, len(ps))
	for i, p := range ps {
		marks[i], args[i] = "?", p
	}
	return "s.provider IN (" + strings.Join(marks, ",") + ")", args
}

const statsFrom = " FROM messages m JOIN sessions s ON s.id = m.session_id"

// Activity devolve sessões e mensagens por intervalo e provider, em ordem.
func (s *IndexStore) Activity(ctx context.Context, f StatsFilter) ([]ActivityRow, error) {
	where, wargs := f.where(true)
	q := "SELECT " + bucketExpr(f.Bucket) + " AS b, s.provider, count(DISTINCT m.session_id), count(*)" +
		statsFrom + where + " GROUP BY b, s.provider ORDER BY b, s.provider"
	rows, err := s.db.QueryContext(ctx, q, append(bucketArgs(f), wargs...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ActivityRow
	for rows.Next() {
		var r ActivityRow
		var b any
		if err := rows.Scan(&b, &r.Provider, &r.Sessions, &r.Messages); err != nil {
			return nil, err
		}
		r.Bucket = bucketKey(f.Bucket, b)
		out = append(out, r)
	}
	return out, rows.Err()
}

// ProjectsPerBucket conta projetos distintos com atividade em cada intervalo.
func (s *IndexStore) ProjectsPerBucket(ctx context.Context, f StatsFilter) (map[string]int, error) {
	where, wargs := f.where(true)
	q := "SELECT " + bucketExpr(f.Bucket) + " AS b, count(DISTINCT s.project_id)" + statsFrom + where +
		" AND s.project_id IS NOT NULL GROUP BY b"
	rows, err := s.db.QueryContext(ctx, q, append(bucketArgs(f), wargs...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var b any
		var n int
		if err := rows.Scan(&b, &n); err != nil {
			return nil, err
		}
		out[bucketKey(f.Bucket, b)] = n
	}
	return out, rows.Err()
}

// StatsTotals devolve os totais distintos do período e as sessões ativas por
// provider.
func (s *IndexStore) StatsTotals(ctx context.Context, f StatsFilter) (Totals, map[domain.ProviderID]int, error) {
	// Uma varredura: por provider e projeto. Cada sessão tem um provider e
	// um projeto, então as sessões distintas somam sem repetir.
	where, args := f.where(true)
	rows, err := s.db.QueryContext(ctx,
		"SELECT s.provider, s.project_id, count(DISTINCT m.session_id), count(*)"+statsFrom+where+
			" GROUP BY s.provider, s.project_id", args...)
	if err != nil {
		return Totals{}, nil, err
	}
	defer rows.Close()
	var t Totals
	by := map[domain.ProviderID]int{}
	projects := map[int64]bool{}
	for rows.Next() {
		var p domain.ProviderID
		var project sql.NullInt64
		var sessions, messages int
		if err := rows.Scan(&p, &project, &sessions, &messages); err != nil {
			return Totals{}, nil, err
		}
		by[p] += sessions
		t.Sessions += sessions
		t.Messages += messages
		if project.Valid {
			projects[project.Int64] = true
		}
	}
	t.Projects = len(projects)
	return t, by, rows.Err()
}

// TopProjects ordena os projetos por mensagens no período.
func (s *IndexStore) TopProjects(ctx context.Context, f StatsFilter, n int) ([]NamedCount, error) {
	where, args := f.where(true)
	return s.ranking(ctx, "SELECT p.name, p.path, count(*) AS c"+statsFrom+
		" JOIN projects p ON p.id = s.project_id"+where+" GROUP BY p.id ORDER BY c DESC, p.path LIMIT ?", append(args, n))
}

// TopTools ordena as ferramentas por número de chamadas no período (todas as
// mensagens: os passos só de ferramenta não contam como mensagem, mas chamam).
func (s *IndexStore) TopTools(ctx context.Context, f StatsFilter, n int) ([]NamedCount, error) {
	where, args := f.where(false)
	return s.ranking(ctx, "SELECT j.value, '', count(*) AS c"+statsFrom+", json_each(m.tools) j"+where+
		" AND m.tools != '[]' GROUP BY j.value ORDER BY c DESC, j.value LIMIT ?", append(args, n))
}

func (s *IndexStore) ranking(ctx context.Context, q string, args []any) ([]NamedCount, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []NamedCount
	for rows.Next() {
		var r NamedCount
		if err := rows.Scan(&r.Name, &r.Path, &r.Count); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// FirstActivity devolve o horário da primeira mensagem visível (zero se não
// há nenhuma).
func (s *IndexStore) FirstActivity(ctx context.Context, providers []domain.ProviderID) (time.Time, error) {
	where, args := StatsFilter{Providers: providers}.where(true)
	var ms sql.NullInt64
	if err := s.db.QueryRowContext(ctx, "SELECT min(m.ts)"+statsFrom+where, args...).Scan(&ms); err != nil || !ms.Valid {
		return time.Time{}, err
	}
	return time.UnixMilli(ms.Int64).UTC(), nil
}

// TotalProjects conta projetos com sessão visível dos providers.
func (s *IndexStore) TotalProjects(ctx context.Context, providers []domain.ProviderID) (int, error) {
	q := "SELECT count(DISTINCT s.project_id) FROM sessions s WHERE s.project_id IS NOT NULL AND " + visibleS
	c, args := providersIn(providers)
	if c != "" {
		q += " AND " + c
	}
	var n int
	return n, s.db.QueryRowContext(ctx, q, args...).Scan(&n)
}

// ActivityHeatmap conta mensagens por dia da semana (segunda = 0) e hora,
// no horário local.
func (s *IndexStore) ActivityHeatmap(ctx context.Context, f StatsFilter) ([7][24]int, error) {
	var h [7][24]int
	where, args := f.where(true)
	// Dia local desde a época (uma quinta: +3 leva a segunda = 0) e hora local.
	q := "SELECT ((m.ts / 1000 + ?) / 86400 + 3) % 7, ((m.ts / 1000 + ?) % 86400) / 3600, count(*)" + statsFrom + where + " GROUP BY 1, 2"
	rows, err := s.db.QueryContext(ctx, q, append([]any{f.Offset, f.Offset}, args...)...)
	if err != nil {
		return h, err
	}
	defer rows.Close()
	for rows.Next() {
		var d, hr, n int
		if err := rows.Scan(&d, &hr, &n); err != nil {
			return h, err
		}
		if d >= 0 && d < 7 && hr >= 0 && hr < 24 {
			h[d][hr] = n
		}
	}
	return h, rows.Err()
}

// ActiveDays conta os dias locais com pelo menos uma mensagem.
func (s *IndexStore) ActiveDays(ctx context.Context, f StatsFilter) (int, error) {
	where, args := f.where(true)
	var n int
	err := s.db.QueryRowContext(ctx, "SELECT count(DISTINCT (m.ts / 1000 + ?) / 86400)"+statsFrom+where,
		append([]any{f.Offset}, args...)...).Scan(&n)
	return n, err
}

// TopProjectsBySessions ordena os projetos por sessões com atividade.
func (s *IndexStore) TopProjectsBySessions(ctx context.Context, f StatsFilter, n int) ([]NamedCount, error) {
	where, args := f.where(true)
	return s.ranking(ctx, "SELECT p.name, p.path, count(DISTINCT m.session_id) AS c"+statsFrom+
		" JOIN projects p ON p.id = s.project_id"+where+" GROUP BY p.id ORDER BY c DESC, p.path LIMIT ?", append(args, n))
}
