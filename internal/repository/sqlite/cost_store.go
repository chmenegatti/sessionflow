package sqlite

import (
	"cmp"
	"context"
	"database/sql"
	"database/sql/driver"
	"net/url"
	"slices"
	"strings"

	"sessionflow/internal/domain"
)

// UsageItem é um registro de consumo a estimar, com a chave estável da
// resposta (EstimateKey).
type UsageItem struct {
	Key    EstimateKey
	Ts     *int64 // ms; nil = sem horário
	Model  string
	Tokens domain.UsageTokens
}

// UsageForEstimate devolve os registros do recorte SEM a deduplicação entre
// sessões: toda cópia tem a própria estimativa (docs/ANALYTICS.md §Custos).
func (s *IndexStore) UsageForEstimate(ctx context.Context, f UsageFilter) ([]UsageItem, error) {
	cond, args := f.conds()
	where := ""
	if len(cond) > 0 {
		where = " WHERE " + strings.Join(cond, " AND ")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT s.provider, s.native_id, u.response_id, u.ts, u.model,
	  u.input_tokens, u.output_tokens, u.cache_read_tokens, u.cache_creation_tokens
	  FROM usage u JOIN sessions s ON s.id = u.session_id`+where+" ORDER BY u.id", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UsageItem
	for rows.Next() {
		var it UsageItem
		var ts, in, outp, cr, cc sql.NullInt64
		if err := rows.Scan(&it.Key.Provider, &it.Key.NativeID, &it.Key.ResponseID, &ts, &it.Model, &in, &outp, &cr, &cc); err != nil {
			return nil, err
		}
		it.Ts = int64Ptr(ts)
		it.Tokens = domain.UsageTokens{Input: int64Ptr(in), Output: int64Ptr(outp), CacheRead: int64Ptr(cr), CacheCreation: int64Ptr(cc)}
		out = append(out, it)
	}
	return out, rows.Err()
}

// CostSums agrega estimativas de uma moeda. Currency "" reúne os registros
// sem custo calculado (sem preço, sem dados ou ainda sem estimativa).
type CostSums struct {
	Currency      string       `json:"currency"`
	Input         domain.Nanos `json:"input"`
	Output        domain.Nanos `json:"output"`
	CacheRead     domain.Nanos `json:"cacheRead"`
	CacheCreation domain.Nanos `json:"cacheCreation"`
	Total         domain.Nanos `json:"total"`
	Records       int          `json:"records"`
	Complete      int          `json:"complete"`
	Partial       int          `json:"partial"`
	NoPrice       int          `json:"noPrice"`
	Insufficient  int          `json:"insufficient"`
	Unestimated   int          `json:"unestimated"` // registro ainda sem estimativa gravada
}

// Add soma o em s (mesma moeda; em Currency "" só as contagens importam).
func (s *CostSums) Add(o CostSums) {
	s.Input += o.Input
	s.Output += o.Output
	s.CacheRead += o.CacheRead
	s.CacheCreation += o.CacheCreation
	s.Total += o.Total
	s.Records += o.Records
	s.Complete += o.Complete
	s.Partial += o.Partial
	s.NoPrice += o.NoPrice
	s.Insufficient += o.Insufficient
	s.Unestimated += o.Unestimated
}

func (s *CostSums) dest() []any {
	return []any{&s.Currency, &s.Input, &s.Output, &s.CacheRead, &s.CacheCreation, &s.Total,
		&s.Records, &s.Complete, &s.Partial, &s.NoPrice, &s.Insufficient, &s.Unestimated}
}

// costCols: moeda e somas, na ordem de CostSums.dest. Só entra na moeda o
// registro com custo calculado; o resto fica em "".
const costCols = `CASE WHEN e.total_cost IS NOT NULL THEN e.currency ELSE '' END AS cur,
  coalesce(sum(e.input_cost), 0), coalesce(sum(e.output_cost), 0),
  coalesce(sum(e.cache_read_cost), 0), coalesce(sum(e.cache_creation_cost), 0),
  coalesce(sum(e.total_cost), 0), count(*),
  count(CASE WHEN e.status = 'complete' THEN 1 END), count(CASE WHEN e.status = 'partial' THEN 1 END),
  count(CASE WHEN e.status = 'no_price' THEN 1 END), count(CASE WHEN e.status = 'insufficient' THEN 1 END),
  count(CASE WHEN e.status IS NULL THEN 1 END)`

const estimateJoin = ` LEFT JOIN u.cost_estimates e
  ON e.provider = d.provider AND e.native_id = d.native_id AND e.response_id = d.response_id`

// CostRow é o custo de uma combinação provider × modelo × projeto ×
// intervalo × moeda.
type CostRow struct {
	Provider    domain.ProviderID
	Model       string
	Project     string
	ProjectName string
	Bucket      string // "" sem intervalo (ou registro sem horário)
	Sums        CostSums
}

// CostSessionRow é o custo de uma sessão raiz (com os descendentes
// associados), por moeda.
type CostSessionRow struct {
	SessionID  int64             `json:"sessionId"`
	Key        domain.SessionKey `json:"key"`
	Title      string            `json:"title"` // só para a interface; o CSV nunca leva o título
	Project    string            `json:"project"`
	Associated bool              `json:"associated"`
	Sums       []CostSums        `json:"sums"`
}

// UncalculableRow é uma sessão raiz com registros sem custo calculável.
type UncalculableRow struct {
	SessionID    int64             `json:"sessionId"`
	Key          domain.SessionKey `json:"key"`
	Title        string            `json:"title"`
	Project      string            `json:"project"`
	NoPrice      int               `json:"noPrice"`
	Insufficient int               `json:"insufficient"`
	Unestimated  int               `json:"unestimated"`
	Reasons      []string          `json:"reasons"`
}

// CostStore faz os relatórios de custo: o consumo deduplicado do índice
// (mesma CTE da Fase 8) junto das estimativas do user.db, anexado somente
// leitura numa conexão dedicada.
type CostStore struct {
	index    *sql.DB
	userPath string
}

// NewCostStore cria o store; userPath é o arquivo do user.db.
func NewCostStore(index *sql.DB, userPath string) *CostStore {
	return &CostStore{index: index, userPath: userPath}
}

// attached roda fn numa conexão com o user.db anexado como "u". O caminho é
// escapado como em dsn: "?", "#" e "%" num diretório abririam outro arquivo.
func (c *CostStore) attached(ctx context.Context, fn func(*sql.Conn) error) error {
	conn, err := c.index.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	uri := "file:" + (&url.URL{Path: c.userPath}).EscapedPath() + "?mode=ro"
	if _, err := conn.ExecContext(ctx, "ATTACH DATABASE ? AS u", uri); err != nil {
		return classify("anexar user.db", err)
	}
	defer func() {
		if _, err := conn.ExecContext(context.Background(), "DETACH DATABASE u"); err != nil {
			// Conexão com o user.db anexado não volta ao pool.
			conn.Raw(func(any) error { return driver.ErrBadConn })
		}
	}()
	return fn(conn)
}

// Breakdown agrupa o custo do recorte por provider, modelo, projeto, moeda
// e, com byBucket, intervalo (f.Bucket, fuso f.Offset). Uma consulta só
// alimenta totais, agrupamentos e série.
func (c *CostStore) Breakdown(ctx context.Context, f UsageFilter, byBucket bool) ([]CostRow, error) {
	with, args, where := f.base()
	bexpr := "NULL"
	if byBucket {
		bexpr = strings.ReplaceAll(bucketExpr(f.Bucket), "m.ts", "d.ts")
		args = append(args, bucketArgs(StatsFilter{Bucket: f.Bucket, Offset: f.Offset})...)
	}
	q := with + "SELECT d.provider, d.model, coalesce(pj.path, ''), coalesce(pj.name, ''), " + bexpr + " AS b, " + costCols +
		" FROM d" + estimateJoin + " LEFT JOIN projects pj ON pj.id = d.project_id" + where + " GROUP BY 1, 2, 3, b, cur"
	var out []CostRow
	err := c.attached(ctx, func(conn *sql.Conn) error {
		rows, err := conn.QueryContext(ctx, q, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r CostRow
			var b any
			if err := rows.Scan(append([]any{&r.Provider, &r.Model, &r.Project, &r.ProjectName, &b}, r.Sums.dest()...)...); err != nil {
				return err
			}
			r.Bucket = bucketKey(f.Bucket, b)
			out = append(out, r)
		}
		return rows.Err()
	})
	return out, err
}

// rootSums: custo por sessão raiz e moeda, com os dados da raiz.
func (c *CostStore) rootSums(ctx context.Context, f UsageFilter, extra string) ([]CostSessionRow, map[int64][]string, error) {
	with, args, where := f.base()
	q := with + `, g AS (SELECT d.root_id, ` + costCols + `, group_concat(DISTINCT e.reasons) AS reasons FROM d` + estimateJoin + where + ` GROUP BY d.root_id, cur)
	SELECT g.*, r.provider, r.native_id, r.title, coalesce(pj.path, ''), r.parent_native_id IS NULL
	FROM g JOIN sessions r ON r.id = g.root_id LEFT JOIN projects pj ON pj.id = r.project_id` + extra
	idx := map[int64]int{}
	var out []CostSessionRow
	reasons := map[int64][]string{}
	err := c.attached(ctx, func(conn *sql.Conn) error {
		rows, err := conn.QueryContext(ctx, q, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			var s CostSums
			var rs sql.NullString
			var r CostSessionRow
			dest := append(append([]any{&id}, s.dest()...), &rs, &r.Key.Provider, &r.Key.NativeID, &r.Title, &r.Project, &r.Associated)
			if err := rows.Scan(dest...); err != nil {
				return err
			}
			i, ok := idx[id]
			if !ok {
				r.SessionID = id
				i = len(out)
				idx[id] = i
				out = append(out, r)
			}
			out[i].Sums = append(out[i].Sums, s)
			for _, part := range strings.FieldsFunc(rs.String, func(r rune) bool { return r == ',' || r == ';' }) {
				if !slices.Contains(reasons[id], part) {
					reasons[id] = append(reasons[id], part)
				}
			}
		}
		return rows.Err()
	})
	return out, reasons, err
}

// primary é a moeda pela qual uma linha entra no ranking: a primeira (em
// ordem alfabética do código) com custo calculado.
func primary(sums []CostSums) (CostSums, bool) {
	var best CostSums
	ok := false
	for _, s := range sums {
		if s.Currency != "" && (!ok || s.Currency < best.Currency) {
			best, ok = s, true
		}
	}
	return best, ok
}

// CompareCostSums ordena linhas de custo (grupos ou sessões) sem comparar
// valores de moedas diferentes: primeiro por moeda (código ISO em ordem
// alfabética; linhas sem custo calculado no fim), depois pelo total
// daquela moeda, do maior para o menor, e por quantidade de respostas.
// Empate devolve 0: quem chama desempata pela chave.
func CompareCostSums(a, b []CostSums) int {
	pa, oka := primary(a)
	pb, okb := primary(b)
	if oka != okb {
		if oka {
			return -1
		}
		return 1
	}
	if oka {
		if c := cmp.Compare(pa.Currency, pb.Currency); c != 0 {
			return c
		}
		if c := cmp.Compare(pb.Total, pa.Total); c != 0 {
			return c
		}
	}
	records := func(ss []CostSums) (n int) {
		for _, s := range ss {
			n += s.Records
		}
		return n
	}
	return cmp.Compare(records(b), records(a))
}

func page[T any](all []T, offset, limit int) []T {
	if offset >= len(all) {
		return []T{}
	}
	return all[offset:min(offset+limit, len(all))]
}

// Sessions lista as sessões raiz com custo por moeda, em ordem de
// CompareCostSums (moedas nunca se comparam), e o total de raízes.
func (c *CostStore) Sessions(ctx context.Context, f UsageFilter, offset, limit int) ([]CostSessionRow, int, error) {
	all, _, err := c.rootSums(ctx, f, "")
	if err != nil {
		return nil, 0, err
	}
	for i := range all {
		slices.SortFunc(all[i].Sums, func(a, b CostSums) int { return cmp.Compare(a.Currency, b.Currency) })
	}
	slices.SortFunc(all, func(a, b CostSessionRow) int {
		return cmp.Or(CompareCostSums(a.Sums, b.Sums), cmp.Compare(a.SessionID, b.SessionID))
	})
	return page(all, offset, limit), len(all), nil
}

// Uncalculable lista as sessões raiz com registros sem custo calculável
// (sem preço, sem dados ou ainda sem estimativa), com os motivos.
func (c *CostStore) Uncalculable(ctx context.Context, f UsageFilter, offset, limit int) ([]UncalculableRow, int, error) {
	all, reasons, err := c.rootSums(ctx, f, " WHERE g.cur = ''")
	if err != nil {
		return nil, 0, err
	}
	out := []UncalculableRow{}
	for _, r := range all {
		u := UncalculableRow{SessionID: r.SessionID, Key: r.Key, Title: r.Title, Project: r.Project, Reasons: reasons[r.SessionID]}
		for _, s := range r.Sums {
			u.NoPrice += s.NoPrice
			u.Insufficient += s.Insufficient
			u.Unestimated += s.Unestimated
		}
		if u.NoPrice+u.Insufficient+u.Unestimated > 0 {
			out = append(out, u)
		}
	}
	slices.SortFunc(out, func(a, b UncalculableRow) int {
		if c := cmp.Compare(b.NoPrice+b.Insufficient+b.Unestimated, a.NoPrice+a.Insufficient+a.Unestimated); c != 0 {
			return c
		}
		return cmp.Compare(a.SessionID, b.SessionID)
	})
	return page(out, offset, limit), len(out), nil
}

// UsageKeys devolve as chaves dos registros do recorte COM a deduplicação
// entre sessões: as respostas que entram nos relatórios.
func (s *IndexStore) UsageKeys(ctx context.Context, f UsageFilter) ([]EstimateKey, error) {
	with, args, where := f.base()
	rows, err := s.db.QueryContext(ctx, with+"SELECT d.provider, d.native_id, d.response_id FROM d"+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EstimateKey
	for rows.Next() {
		var k EstimateKey
		if err := rows.Scan(&k.Provider, &k.NativeID, &k.ResponseID); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}
