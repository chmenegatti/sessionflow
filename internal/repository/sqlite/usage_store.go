package sqlite

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"
	"time"

	"sessionflow/internal/domain"
)

// UsageFilter recorta o consumo. Ao contrário das estatísticas de atividade
// (StatsFilter), não filtra por visibilidade: subagentes e sessões
// automáticas entram (regra de domínio, docs/ANALYTICS.md).
type UsageFilter struct {
	From, To  time.Time           // [From, To); registros sem horário ficam de fora
	NoTime    bool                // ignora From/To (inclui registros sem horário)
	Providers []domain.ProviderID // vazio = todos
	Project   string              // caminho; vazio = todos
	Model     string              // vazio = todos
	Bucket    Bucket              // UsageSeries
	Offset    int                 // segundos do fuso local
}

// Field é a soma de um campo e quantos registros o informaram. Count menor
// que Records = parcial; 0 = indisponível (Sum 0 não significa zero tokens).
type Field struct {
	Sum   int64 `json:"sum"`
	Count int   `json:"count"`
}

// TokenSums agrega registros de consumo.
type TokenSums struct {
	Records       int   `json:"records"`
	Input         Field `json:"input"`
	Output        Field `json:"output"`
	CacheRead     Field `json:"cacheRead"`
	CacheCreation Field `json:"cacheCreation"`
	Reasoning     Field `json:"reasoning"`
	Total         Field `json:"total"`
	Derived       int   `json:"derived"` // registros com total derivado
}

// UsageDim é a dimensão de agrupamento de UsageBy.
type UsageDim string

const (
	UsageByProvider UsageDim = "provider"
	UsageByModel    UsageDim = "model"
	UsageByProject  UsageDim = "project"
	UsageByOrigin   UsageDim = "origin" // main_agent | subagent | automatic
)

// UsageGroup é um grupo de UsageBy. Key é o valor (caminho, para projeto).
type UsageGroup struct {
	Key    string    `json:"key"`
	Label  string    `json:"label"`
	Tokens TokenSums `json:"tokens"`
}

// UsagePoint é o total de tokens de um provider num intervalo.
type UsagePoint struct {
	Bucket   string            `json:"bucket"`
	Provider domain.ProviderID `json:"provider"`
	Records  int               `json:"records"`
	Total    Field             `json:"total"`
}

// UsageSessionRow é uma sessão raiz com o consumo consolidado dos
// descendentes associados.
type UsageSessionRow struct {
	SessionID  int64             `json:"sessionId"`
	Key        domain.SessionKey `json:"key"`
	Title      string            `json:"title"`
	Project    string            `json:"project"`
	Origin     string            `json:"origin"`
	Associated bool              `json:"associated"` // false: subordinada sem a sessão pai no índice
	Tokens     TokenSums         `json:"tokens"`
	FirstTs    *int64            `json:"firstTs"` // ms; primeira e última mensagem com horário da raiz
	LastTs     *int64            `json:"lastTs"`
}

// OriginShare é o consumo de uma origem dentro de uma sessão raiz.
type OriginShare struct {
	Origin   string    `json:"origin"`
	Sessions int       `json:"sessions"`
	Tokens   TokenSums `json:"tokens"`
}

// base monta a CTE "d": registros do recorte (filtros aplicados direto na
// tabela, para usar o índice de ts), sem a mesma resposta repetida em
// sessões diferentes do provider (ex.: cópia de contexto num subagente),
// com origin e root_id. where volta vazio: os filtros já estão na CTE.
func (f UsageFilter) base() (with string, args []any, where string) {
	cond := []string{
		// A mesma resposta em outra sessão do provider com prioridade maior
		// (com horário antes de sem horário, para não sumir dos períodos com
		// data; depois principal antes de subagente, a mais antiga e a de
		// menor sessão) exclui esta. response_id vazio não é identidade. Só as respostas repetidas (dups, poucas)
		// passam pela comparação; sondar todas custava ~20 ms no índice real.
		`(u.response_id NOT IN (SELECT response_id FROM dups) OR NOT EXISTS (
		  SELECT 1 FROM usage u2 INDEXED BY usage_resp_idx CROSS JOIN sessions s2 ON s2.id = u2.session_id
		  WHERE u2.response_id = u.response_id AND u2.id != u.id AND s2.provider = s.provider
		    AND ((u2.ts IS NULL), (s2.parent_native_id IS NOT NULL), coalesce(u2.ts, 9e18), u2.session_id)
		      < ((u.ts IS NULL), (s.parent_native_id IS NOT NULL), coalesce(u.ts, 9e18), u.session_id)))`,
	}
	more, fargs := f.conds()
	cond = append(cond, more...)
	args = append(args, fargs...)
	return `WITH RECURSIVE dups AS (SELECT response_id FROM usage WHERE response_id != '' GROUP BY response_id HAVING count(*) > 1),
	-- Sobe a cadeia de pais (subagente dentro de subagente) até a sessão
	-- raiz: a que não tem pai, ou cujo pai não está no índice. Profundidade
	-- limitada: um ciclo nos dados não trava a consulta (fica sem raiz e
	-- conta como a própria sessão).
	up(id, root_id, provider, parent, depth) AS (
	  SELECT id, id, provider, parent_native_id, 0 FROM sessions
	  UNION ALL
	  SELECT up.id, p.id, p.provider, p.parent_native_id, up.depth + 1
	  FROM up JOIN sessions p ON p.provider = up.provider AND p.native_id = up.parent
	  WHERE up.parent IS NOT NULL AND up.depth < 16),
	roots AS (
	  SELECT up.id, up.root_id FROM up
	  WHERE up.parent IS NULL
	     OR NOT EXISTS (SELECT 1 FROM sessions p WHERE p.provider = up.provider AND p.native_id = up.parent)),
	d AS (
	  SELECT u.*, s.provider, s.native_id, s.project_id, s.parent_native_id, s.automated,
	    CASE WHEN s.parent_native_id IS NOT NULL THEN 'subagent'
	         WHEN s.automated = 1 THEN 'automatic' ELSE 'main_agent' END AS origin,
	    coalesce(r.root_id, u.session_id) AS root_id
	  FROM usage u JOIN sessions s ON s.id = u.session_id
	  LEFT JOIN roots r ON r.id = u.session_id
	  WHERE ` + strings.Join(cond, " AND ") + `)
	`, args, ""
}

// conds são os filtros do recorte sobre usage u JOIN sessions s, sem a
// deduplicação entre sessões (UsageForEstimate usa só eles).
func (f UsageFilter) conds() (cond []string, args []any) {
	if !f.NoTime {
		if !f.From.IsZero() {
			cond = append(cond, "u.ts >= ?")
			args = append(args, f.From.UnixMilli())
		}
		if !f.To.IsZero() {
			cond = append(cond, "u.ts < ?")
			args = append(args, f.To.UnixMilli())
		}
	}
	if len(f.Providers) > 0 {
		marks := make([]string, len(f.Providers))
		for i, p := range f.Providers {
			marks[i] = "?"
			args = append(args, p)
		}
		cond = append(cond, "s.provider IN ("+strings.Join(marks, ",")+")")
	}
	if f.Project != "" {
		cond = append(cond, "s.project_id = (SELECT id FROM projects WHERE path = ?)")
		args = append(args, f.Project)
	}
	if f.Model != "" {
		cond = append(cond, "u.model = ?")
		args = append(args, f.Model)
	}
	return cond, args
}

var errUnknownDim = errors.New("dimensão desconhecida")

// sumCols são as colunas agregadas, na ordem de scanSums.
const sumCols = `count(*),
  coalesce(sum(d.input_tokens), 0), count(d.input_tokens),
  coalesce(sum(d.output_tokens), 0), count(d.output_tokens),
  coalesce(sum(d.cache_read_tokens), 0), count(d.cache_read_tokens),
  coalesce(sum(d.cache_creation_tokens), 0), count(d.cache_creation_tokens),
  coalesce(sum(d.reasoning_tokens), 0), count(d.reasoning_tokens),
  coalesce(sum(d.total_tokens), 0), count(d.total_tokens),
  count(CASE WHEN instr(',' || d.derived || ',', ',total,') > 0 THEN 1 END)`

func sumDest(t *TokenSums) []any {
	return []any{&t.Records,
		&t.Input.Sum, &t.Input.Count, &t.Output.Sum, &t.Output.Count,
		&t.CacheRead.Sum, &t.CacheRead.Count, &t.CacheCreation.Sum, &t.CacheCreation.Count,
		&t.Reasoning.Sum, &t.Reasoning.Count, &t.Total.Sum, &t.Total.Count, &t.Derived}
}

// UsageTotals soma o consumo do recorte.
func (s *IndexStore) UsageTotals(ctx context.Context, f UsageFilter) (TokenSums, error) {
	with, args, where := f.base()
	var t TokenSums
	err := s.db.QueryRowContext(ctx, with+"SELECT "+sumCols+" FROM d"+where, args...).Scan(sumDest(&t)...)
	return t, err
}

// UsageBy agrupa o consumo por dimensão, do maior total para o menor.
func (s *IndexStore) UsageBy(ctx context.Context, f UsageFilter, dim UsageDim, limit int) ([]UsageGroup, error) {
	with, args, where := f.base()
	var key, label, join string
	switch dim {
	case UsageByProvider:
		key, label = "d.provider", "d.provider"
	case UsageByModel:
		key, label = "d.model", "d.model"
	case UsageByProject:
		key, label, join = "coalesce(pj.path, '')", "coalesce(pj.name, '')", " LEFT JOIN projects pj ON pj.id = d.project_id"
	case UsageByOrigin:
		key, label = "d.origin", "d.origin"
	default:
		return nil, errUnknownDim
	}
	q := with + "SELECT " + key + ", " + label + ", " + sumCols + " FROM d" + join + where +
		" GROUP BY 1 ORDER BY coalesce(sum(d.total_tokens), 0) DESC, count(*) DESC, 1 LIMIT ?"
	rows, err := s.db.QueryContext(ctx, q, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []UsageGroup{}
	for rows.Next() {
		var g UsageGroup
		if err := rows.Scan(append([]any{&g.Key, &g.Label}, sumDest(&g.Tokens)...)...); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// UsageSeries devolve o total por intervalo e provider (só registros com
// horário).
func (s *IndexStore) UsageSeries(ctx context.Context, f UsageFilter) ([]UsagePoint, error) {
	with, args, where := f.base()
	expr := strings.ReplaceAll(bucketExpr(f.Bucket), "m.ts", "d.ts")
	bargs := bucketArgs(StatsFilter{Bucket: f.Bucket, Offset: f.Offset})
	if where == "" {
		where = " WHERE d.ts IS NOT NULL"
	} else {
		where += " AND d.ts IS NOT NULL"
	}
	q := with + "SELECT " + expr + " AS b, d.provider, count(*), coalesce(sum(d.total_tokens), 0), count(d.total_tokens) FROM d" +
		where + " GROUP BY b, d.provider ORDER BY b, d.provider"
	// Na consulta, os filtros (CTE) vêm antes da expressão do intervalo.
	rows, err := s.db.QueryContext(ctx, q, append(args, bargs...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UsagePoint
	for rows.Next() {
		var p UsagePoint
		var b any
		if err := rows.Scan(&b, &p.Provider, &p.Records, &p.Total.Sum, &p.Total.Count); err != nil {
			return nil, err
		}
		p.Bucket = bucketKey(f.Bucket, b)
		out = append(out, p)
	}
	return out, rows.Err()
}

// UsageSessions lista as sessões raiz (principal, automática sem pai ou
// subordinada sem pai no índice) com o consumo consolidado, do maior total
// para o menor, e o total de raízes.
func (s *IndexStore) UsageSessions(ctx context.Context, f UsageFilter, offset, limit int) ([]UsageSessionRow, int, error) {
	with, args, where := f.base()
	var total int
	if err := s.db.QueryRowContext(ctx, with+"SELECT count(DISTINCT d.root_id) FROM d"+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	q := with + `, g AS (SELECT d.root_id, ` + sumCols + ` FROM d` + where + ` GROUP BY d.root_id)
	SELECT g.*, r.provider, r.native_id, r.title, coalesce(pj.path, ''),
	  CASE WHEN r.parent_native_id IS NOT NULL THEN 'subagent' WHEN r.automated = 1 THEN 'automatic' ELSE 'main_agent' END,
	  r.parent_native_id IS NULL,
	  (SELECT min(ts) FROM messages WHERE session_id = r.id), (SELECT max(ts) FROM messages WHERE session_id = r.id)
	FROM g JOIN sessions r ON r.id = g.root_id LEFT JOIN projects pj ON pj.id = r.project_id
	ORDER BY 13 DESC, g.root_id LIMIT ? OFFSET ?` // 13 = soma do total (root_id + 12 colunas de sumCols)
	rows, err := s.db.QueryContext(ctx, q, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []UsageSessionRow
	for rows.Next() {
		var r UsageSessionRow
		var first, last sql.NullInt64
		dest := append([]any{&r.SessionID}, sumDest(&r.Tokens)...)
		dest = append(dest, &r.Key.Provider, &r.Key.NativeID, &r.Title, &r.Project, &r.Origin, &r.Associated, &first, &last)
		if err := rows.Scan(dest...); err != nil {
			return nil, 0, err
		}
		if first.Valid {
			r.FirstTs = &first.Int64
		}
		if last.Valid {
			r.LastTs = &last.Int64
		}
		out = append(out, r)
	}
	return out, total, rows.Err()
}

// UsageSessionOrigins detalha o consumo de uma sessão raiz por origem.
func (s *IndexStore) UsageSessionOrigins(ctx context.Context, f UsageFilter, rootID int64) ([]OriginShare, error) {
	with, args, where := f.base()
	if where == "" {
		where = " WHERE d.root_id = ?"
	} else {
		where += " AND d.root_id = ?"
	}
	q := with + "SELECT d.origin, count(DISTINCT d.session_id), " + sumCols + " FROM d" + where + " GROUP BY d.origin ORDER BY d.origin"
	rows, err := s.db.QueryContext(ctx, q, append(args, rootID)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []OriginShare{}
	for rows.Next() {
		var o OriginShare
		if err := rows.Scan(append([]any{&o.Origin, &o.Sessions}, sumDest(&o.Tokens)...)...); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// UsageModels lista os modelos com consumo registrado.
func (s *IndexStore) UsageModels(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT DISTINCT model FROM usage WHERE model != '' ORDER BY model")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// UsageFirst devolve o horário do primeiro registro de consumo dos providers
// (zero se não há).
func (s *IndexStore) UsageFirst(ctx context.Context, providers []domain.ProviderID) (time.Time, error) {
	with, args, where := UsageFilter{NoTime: true, Providers: providers}.base()
	var ms sql.NullInt64
	if err := s.db.QueryRowContext(ctx, with+"SELECT min(d.ts) FROM d"+where, args...).Scan(&ms); err != nil || !ms.Valid {
		return time.Time{}, err
	}
	return time.UnixMilli(ms.Int64).UTC(), nil
}

// BreakdownRow é o consumo de uma combinação provider × modelo × projeto ×
// origem. Uma consulta só alimenta totais e todos os agrupamentos (Group,
// Sum): cada consulta sobre o consumo custa ~35 ms no índice real.
type BreakdownRow struct {
	Provider    domain.ProviderID
	Model       string
	ProjectPath string
	ProjectName string
	Origin      string
	Tokens      TokenSums
}

// UsageBreakdown agrupa o recorte por provider, modelo, projeto e origem.
func (s *IndexStore) UsageBreakdown(ctx context.Context, f UsageFilter) ([]BreakdownRow, error) {
	with, args, where := f.base()
	q := with + "SELECT d.provider, d.model, coalesce(pj.path, ''), coalesce(pj.name, ''), d.origin, " + sumCols +
		" FROM d LEFT JOIN projects pj ON pj.id = d.project_id" + where + " GROUP BY 1, 2, 3, 5"
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BreakdownRow
	for rows.Next() {
		var r BreakdownRow
		dest := append([]any{&r.Provider, &r.Model, &r.ProjectPath, &r.ProjectName, &r.Origin}, sumDest(&r.Tokens)...)
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (t *TokenSums) add(o TokenSums) {
	t.Records += o.Records
	t.Derived += o.Derived
	for _, p := range []struct{ a, b *Field }{
		{&t.Input, &o.Input}, {&t.Output, &o.Output}, {&t.CacheRead, &o.CacheRead},
		{&t.CacheCreation, &o.CacheCreation}, {&t.Reasoning, &o.Reasoning}, {&t.Total, &o.Total},
	} {
		p.a.Sum += p.b.Sum
		p.a.Count += p.b.Count
	}
}

// Sum soma as linhas (= UsageTotals do mesmo recorte).
func Sum(rows []BreakdownRow) TokenSums {
	var t TokenSums
	for _, r := range rows {
		t.add(r.Tokens)
	}
	return t
}

// Group agrega as linhas por dimensão, na ordem de UsageBy (maior total,
// mais registros, chave).
func Group(rows []BreakdownRow, dim UsageDim, limit int) []UsageGroup {
	idx := map[string]int{}
	out := []UsageGroup{}
	for _, r := range rows {
		var key, label string
		switch dim {
		case UsageByProvider:
			key, label = string(r.Provider), string(r.Provider)
		case UsageByModel:
			key, label = r.Model, r.Model
		case UsageByProject:
			key, label = r.ProjectPath, r.ProjectName
		case UsageByOrigin:
			key, label = r.Origin, r.Origin
		}
		i, ok := idx[key]
		if !ok {
			i = len(out)
			idx[key] = i
			out = append(out, UsageGroup{Key: key, Label: label})
		}
		out[i].Tokens.add(r.Tokens)
	}
	slices.SortFunc(out, func(a, b UsageGroup) int {
		if c := cmp.Compare(b.Tokens.Total.Sum, a.Tokens.Total.Sum); c != 0 {
			return c
		}
		if c := cmp.Compare(b.Tokens.Records, a.Tokens.Records); c != 0 {
			return c
		}
		return cmp.Compare(a.Key, b.Key)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// Add soma o em t (campo a campo, com as contagens).
func (t *TokenSums) Add(o TokenSums) { t.add(o) }

// UsageRecord é um registro de consumo de uma sessão raiz, já
// deduplicado (mesma CTE dos relatórios). Tokens tem Records = 1.
type UsageRecord struct {
	Ts     *int64 // ms; nil = sem horário
	Tokens TokenSums
}

// UsageSessionRecords lista os registros do recorte da sessão raiz (com os
// descendentes associados), em ordem de horário; sem horário no fim.
func (s *IndexStore) UsageSessionRecords(ctx context.Context, f UsageFilter, rootID int64) ([]UsageRecord, error) {
	with, args, where := f.base()
	if where == "" {
		where = " WHERE d.root_id = ?"
	} else {
		where += " AND d.root_id = ?"
	}
	q := with + "SELECT d.ts, " + sumCols + " FROM d" + where + " GROUP BY d.id ORDER BY d.ts IS NULL, d.ts, d.id"
	rows, err := s.db.QueryContext(ctx, q, append(args, rootID)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UsageRecord
	for rows.Next() {
		var r UsageRecord
		var ts sql.NullInt64
		if err := rows.Scan(append([]any{&ts}, sumDest(&r.Tokens)...)...); err != nil {
			return nil, err
		}
		if ts.Valid {
			r.Ts = &ts.Int64
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
