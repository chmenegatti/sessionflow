package sqlite_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"

	"sessionflow/internal/domain"
	"sessionflow/internal/indexer/indexertest"
	"sessionflow/internal/repository/sqlite"
)

type costEnv struct {
	index  *sqlite.IndexStore
	db     *sql.DB
	prices *sqlite.PriceStore
	costs  *sqlite.CostStore
	items  []sqlite.UsageItem
}

func newCostEnv(t *testing.T, userDir string) *costEnv {
	t.Helper()
	e := indexertest.NewUsageEnv(t)
	e.Run(t)
	if userDir == "" {
		userDir = t.TempDir()
	}
	if err := os.MkdirAll(userDir, 0o700); err != nil {
		t.Fatal(err)
	}
	userPath := filepath.Join(userDir, "user.db")
	udb, err := sqlite.OpenUser(context.Background(), userPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { udb.Close() })
	c := &costEnv{index: e.Store, db: e.DB, prices: sqlite.NewPriceStore(udb), costs: sqlite.NewCostStore(e.DB, userPath)}
	c.items, err = e.Store.UsageForEstimate(context.Background(), sqlite.UsageFilter{NoTime: true})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func strp(s string) *string { return &s }

// estimateAll cadastra um preço "*" (qualquer modelo) para o provider e grava as estimativas
// de todos os registros (como o Refresh faria).
func (c *costEnv) estimateAll(t *testing.T, prices map[domain.ProviderID]string) map[sqlite.EstimateKey]domain.Estimate {
	t.Helper()
	ctx := context.Background()
	var revs []domain.PriceRevision
	for p, cur := range prices {
		r := domain.PriceRevision{PriceKey: string(p), Provider: p, Model: "*", Currency: cur, UnitTokens: 1_000_000,
			Input: strp("3"), Output: strp("15"), CacheRead: strp("0.3"), CacheCreation: strp("3.75"),
			ValidFrom: time.Unix(0, 0).UTC(), CreatedAt: time.Now().UTC()}
		id, err := c.prices.InsertRevision(ctx, r)
		if err != nil {
			t.Fatal(err)
		}
		r.ID = id
		revs = append(revs, r)
	}
	out := map[sqlite.EstimateKey]domain.Estimate{}
	var stored []sqlite.StoredEstimate
	for _, it := range c.items {
		var rev *domain.PriceRevision
		if it.Ts != nil {
			rev = domain.MatchPrice(revs, it.Key.Provider, it.Model, time.UnixMilli(*it.Ts))
		}
		e := domain.ComputeEstimate(rev, it.Key.Provider, it.Model, it.Ts != nil, it.Tokens)
		out[it.Key] = e
		stored = append(stored, sqlite.StoredEstimate{Key: it.Key, Estimate: e, UsageTs: it.Ts, CalculatedAt: time.Now()})
	}
	if err := c.prices.SaveEstimates(ctx, stored); err != nil {
		t.Fatal(err)
	}
	return out
}

func sumByCurrency(rows []sqlite.CostRow) map[string]sqlite.CostSums {
	out := map[string]sqlite.CostSums{}
	for _, r := range rows {
		s := out[r.Sums.Currency]
		s.Currency = r.Sums.Currency
		s.Add(r.Sums)
		out[r.Sums.Currency] = s
	}
	return out
}

func TestUsageForEstimateIncludesCopies(t *testing.T) {
	c := newCostEnv(t, "")
	var n int
	c.db.QueryRow("SELECT count(*) FROM usage").Scan(&n)
	tot, _ := c.index.UsageTotals(context.Background(), sqlite.UsageFilter{NoTime: true})
	if len(c.items) != n || n <= tot.Records {
		t.Errorf("itens %d, usage %d, deduplicados %d", len(c.items), n, tot.Records)
	}
	for _, it := range c.items {
		if it.Key.NativeID == "" || it.Key.Provider == "" {
			t.Errorf("chave incompleta %+v", it.Key)
		}
	}
}

func TestCostBreakdownDedup(t *testing.T) {
	c := newCostEnv(t, "")
	est := c.estimateAll(t, map[domain.ProviderID]string{domain.ProviderClaude: "USD"})
	f := sqlite.UsageFilter{NoTime: true, Providers: []domain.ProviderID{domain.ProviderClaude}}
	rows, err := c.costs.Breakdown(context.Background(), f, false)
	if err != nil {
		t.Fatal(err)
	}
	// Soma esperada: todas as estimativas do Claude, a resposta repetida
	// (cópia no subagente) uma vez só.
	seen := map[string]bool{}
	var want int64
	for k, e := range est {
		if k.Provider != domain.ProviderClaude || e.Total == nil || seen[k.ResponseID] {
			continue
		}
		seen[k.ResponseID] = true
		want += int64(*e.Total)
	}
	by := sumByCurrency(rows)
	tot, _ := c.index.UsageTotals(context.Background(), f)
	records := 0
	for _, s := range by {
		records += s.Records
	}
	if int64(by["USD"].Total) != want || records != tot.Records || want == 0 {
		t.Errorf("USD %s (quer %s); registros %d (quer %d); %+v", by["USD"].Total, domain.Nanos(want), records, tot.Records, by)
	}
	// O registro sem cache criado é parcial e tem custo (moeda USD).
	if by["USD"].Partial != 1 || by["USD"].Complete != 6 {
		t.Errorf("status %+v", by["USD"])
	}
}

func TestCostSessionsReconcile(t *testing.T) {
	c := newCostEnv(t, "")
	c.estimateAll(t, map[domain.ProviderID]string{domain.ProviderClaude: "USD", domain.ProviderCodex: "BRL"})
	f := sqlite.UsageFilter{NoTime: true}
	rows, _ := c.costs.Breakdown(context.Background(), f, false)
	want := sumByCurrency(rows)
	sess, total, err := c.costs.Sessions(context.Background(), f, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]domain.Nanos{}
	for _, r := range sess {
		for _, s := range r.Sums {
			got[s.Currency] += s.Total
		}
	}
	if total != len(sess) || got["USD"] != want["USD"].Total || got["BRL"] != want["BRL"].Total || got["USD"] == 0 {
		t.Errorf("sessões %v; breakdown %v (total %d, linhas %d)", got, want, total, len(sess))
	}
	// Ordenação: por moeda (BRL antes de USD), maior total da moeda
	// primeiro; nunca a soma de moedas diferentes.
	cur := func(r sqlite.CostSessionRow) string {
		c := "~"
		for _, s := range r.Sums {
			if s.Currency != "" && s.Currency < c {
				c = s.Currency
			}
		}
		return c
	}
	for i := 1; i < len(sess); i++ {
		if sqlite.CompareCostSums(sess[i-1].Sums, sess[i].Sums) > 0 {
			t.Errorf("fora de ordem em %d", i)
		}
		if cur(sess[i]) < cur(sess[i-1]) {
			t.Errorf("moeda %s depois de %s em %d", cur(sess[i]), cur(sess[i-1]), i)
		}
	}
	page, total2, _ := c.costs.Sessions(context.Background(), f, 1, 1)
	if len(page) != 1 || total2 != total || page[0].SessionID != sess[1].SessionID {
		t.Errorf("página: %+v", page)
	}
}

func TestCostUncalculable(t *testing.T) {
	c := newCostEnv(t, "")
	c.estimateAll(t, map[domain.ProviderID]string{domain.ProviderClaude: "USD"})
	rows, total, err := c.costs.Uncalculable(context.Background(), sqlite.UsageFilter{NoTime: true}, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range rows {
		if r.Key.Provider == domain.ProviderClaude {
			t.Errorf("Claude tem preço: %+v", r)
		}
		if r.Key.Provider == domain.ProviderOpenCode && r.NoPrice > 0 && slices.Contains(r.Reasons, domain.ReasonNoPrice) {
			found = true
		}
	}
	if !found || total != len(rows) {
		t.Errorf("OpenCode sem preço ausente: %+v", rows)
	}
	// Sem estimativa gravada: conta como não calculado.
	c2 := newCostEnv(t, "")
	rows2, _, _ := c2.costs.Uncalculable(context.Background(), sqlite.UsageFilter{NoTime: true}, 0, 100)
	un := 0
	for _, r := range rows2 {
		un += r.Unestimated
	}
	if un != func() int {
		tot, _ := c2.index.UsageTotals(context.Background(), sqlite.UsageFilter{NoTime: true})
		return tot.Records
	}() {
		t.Errorf("não calculados %d", un)
	}
}

func TestCostBreakdownBuckets(t *testing.T) {
	c := newCostEnv(t, "")
	c.estimateAll(t, map[domain.ProviderID]string{domain.ProviderClaude: "USD"})
	f := year
	f.Providers = []domain.ProviderID{domain.ProviderClaude}
	plain, _ := c.costs.Breakdown(context.Background(), f, false)
	bucketed, err := c.costs.Breakdown(context.Background(), f, true)
	if err != nil {
		t.Fatal(err)
	}
	if sumByCurrency(plain)["USD"] != sumByCurrency(bucketed)["USD"] {
		t.Errorf("série ≠ total")
	}
	for _, r := range bucketed {
		if len(r.Bucket) != len("2026-11-25") {
			t.Errorf("intervalo %q", r.Bucket)
		}
	}
}

func TestCostStoreAttachEscapedPath(t *testing.T) {
	name := "a?b#c%d e"
	if runtime.GOOS == "windows" {
		name = "a#c%d e" // "?" não é permitido em nomes de arquivo do Windows
	}
	c := newCostEnv(t, filepath.Join(t.TempDir(), name))
	c.estimateAll(t, map[domain.ProviderID]string{domain.ProviderClaude: "USD"})
	rows, err := c.costs.Breakdown(context.Background(), sqlite.UsageFilter{NoTime: true}, false)
	if err != nil || sumByCurrency(rows)["USD"].Total == 0 {
		t.Errorf("%v %v", sumByCurrency(rows), err)
	}
}

func TestCostStoreDetaches(t *testing.T) {
	c := newCostEnv(t, "")
	for range 3 {
		if _, err := c.costs.Breakdown(context.Background(), sqlite.UsageFilter{NoTime: true}, false); err != nil {
			t.Fatal(err)
		}
	}
	c.db.SetMaxOpenConns(1) // força reutilizar a conexão usada acima
	rows, err := c.db.Query("PRAGMA database_list")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var seq int
		var name, file string
		rows.Scan(&seq, &name, &file)
		if name == "u" {
			t.Error("user.db continua anexado")
		}
	}
}

func TestUsageKeysDedup(t *testing.T) {
	c := newCostEnv(t, "")
	f := sqlite.UsageFilter{NoTime: true}
	keys, err := c.index.UsageKeys(context.Background(), f)
	tot, _ := c.index.UsageTotals(context.Background(), f)
	if err != nil || len(keys) != tot.Records || len(keys) >= len(c.items) {
		t.Errorf("%d chaves, %d registros, %d itens (%v)", len(keys), tot.Records, len(c.items), err)
	}
}
