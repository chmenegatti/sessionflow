package service

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"sessionflow/internal/config"
	"sessionflow/internal/domain"
	"sessionflow/internal/indexer/indexertest"
	"sessionflow/internal/repository/sqlite"
)

type costEnv struct {
	svc    *CostService
	index  *sql.DB
	user   *sql.DB
	prices *sqlite.PriceStore
}

func newCostEnv(t *testing.T) *costEnv {
	t.Helper()
	e := indexertest.NewUsageEnv(t)
	e.Run(t)
	dir := t.TempDir()
	userPath := filepath.Join(dir, "user.db")
	udb, err := sqlite.OpenUser(context.Background(), userPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { udb.Close() })
	user := sqlite.NewUserStore(udb)
	stats := NewStatsService(e.Store, NewSessionService(e.Store, user, discard), user, discard)
	stats.now = func() time.Time { return statNow }
	a := NewAnalyticsService(e.Store, stats, discard)
	a.now = func() time.Time { return statNow }
	prices := sqlite.NewPriceStore(udb)
	svc := NewCostService(CostDeps{
		Index: e.Store, Costs: sqlite.NewCostStore(e.DB, userPath), Prices: prices, Analytics: a,
		Config: NewConfigStore(filepath.Join(dir, "config.json"), config.Default(), nil), Log: discard,
	})
	svc.now = func() time.Time { return statNow }
	svc.async = func(fn func()) { fn() }
	return &costEnv{svc: svc, index: e.DB, user: udb, prices: prices}
}

func ptr(s string) *string { return &s }

var claudeUSD = PriceInput{Provider: domain.ProviderClaude, Model: "claude-*", Currency: "usd", UnitTokens: 1_000_000,
	Input: ptr("3"), Output: ptr("15"), CacheRead: ptr("0.30"), CacheCreation: ptr("3.75"), ValidFrom: "2026-01-01", Source: "teste"}

func doubled(in PriceInput) PriceInput {
	in.Input, in.Output, in.CacheRead, in.CacheCreation = ptr("6"), ptr("30"), ptr("0.6"), ptr("7.5")
	return in
}

func (c *costEnv) total(t *testing.T, q AnalyticsQuery, cur string) domain.Nanos {
	t.Helper()
	v, err := c.svc.Costs(context.Background(), q, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range v.Totals {
		if s.Currency == cur {
			return s.Total
		}
	}
	return 0
}

func (c *costEnv) add(t *testing.T, in PriceInput) domain.PriceRevision {
	t.Helper()
	r, err := c.svc.AddPrice(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestCostEditPriceKeepsEstimates(t *testing.T) {
	c := newCostEnv(t)
	r := c.add(t, claudeUSD)
	before := c.total(t, allClaude, "USD")
	if before == 0 {
		t.Fatal("sem custo depois de cadastrar o preço")
	}
	if _, err := c.svc.EditPrice(context.Background(), r.ID, doubled(claudeUSD)); err != nil {
		t.Fatal(err)
	}
	if after := c.total(t, allClaude, "USD"); after != before {
		t.Errorf("edição mudou o custo antigo: %s → %s", before, after)
	}
	es, _ := c.prices.Estimates(context.Background())
	for k, e := range es {
		if k.Provider == domain.ProviderClaude && e.RevisionID != nil && *e.RevisionID != r.ID {
			t.Errorf("%v na revisão %d", k, *e.RevisionID)
		}
	}
}

func TestCostRecalculateAppliesCurrent(t *testing.T) {
	c := newCostEnv(t)
	ctx := context.Background()
	r := c.add(t, claudeUSD)
	before := c.total(t, allClaude, "USD")
	c.svc.EditPrice(ctx, r.ID, doubled(claudeUSD))
	p, err := c.svc.PreviewRecalculate(ctx, allClaude)
	if err != nil {
		t.Fatal(err)
	}
	if p.Changes == 0 || len(p.After) != 1 || p.After[0].Total != 2*before || p.Before[0].Total != before {
		t.Errorf("prévia %+v (antes %s)", p, before)
	}
	if _, err := c.recalc(ctx); err != nil {
		t.Fatal(err)
	}
	if got := c.total(t, allClaude, "USD"); got != 2*before {
		t.Errorf("depois do recálculo %s; quer %s", got, 2*before)
	}
	if p, _ := c.svc.PreviewRecalculate(ctx, allClaude); p.Changes != 0 {
		t.Errorf("segunda prévia: %d mudanças", p.Changes)
	}
}

func TestCostNoPriceAutoPriced(t *testing.T) {
	c := newCostEnv(t)
	ctx := context.Background()
	if _, err := c.svc.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	v, _ := c.svc.Costs(ctx, allClaude, "")
	if v.HasPrices || len(v.Totals) != 0 || v.Quality.NoPrice != v.Quality.Records || v.Quality.Records == 0 {
		t.Errorf("sem preços: %+v", v)
	}
	c.add(t, claudeUSD) // sem Refresh manual: o cadastro dispara
	v, _ = c.svc.Costs(ctx, allClaude, "")
	if !v.HasPrices || v.Quality.Complete == 0 || v.Quality.NoPrice != 0 {
		t.Errorf("depois do preço: %+v", v.Quality)
	}
}

func TestCostGrowingTokensKeepRevision(t *testing.T) {
	c := newCostEnv(t)
	ctx := context.Background()
	r := c.add(t, claudeUSD)
	c.svc.EditPrice(ctx, r.ID, doubled(claudeUSD))
	var key sqlite.EstimateKey
	var id int64
	if err := c.index.QueryRow(`SELECT u.id, s.provider, s.native_id, u.response_id FROM usage u JOIN sessions s ON s.id = u.session_id
	  WHERE s.provider = 'claude' AND u.output_tokens IS NOT NULL ORDER BY u.id LIMIT 1`).Scan(&id, &key.Provider, &key.NativeID, &key.ResponseID); err != nil {
		t.Fatal(err)
	}
	es, _ := c.prices.Estimates(ctx)
	old := es[key]
	if _, err := c.index.Exec("UPDATE usage SET output_tokens = output_tokens + 1000 WHERE id = ?", id); err != nil {
		t.Fatal(err)
	}
	if _, err := c.svc.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	es, _ = c.prices.Estimates(ctx)
	got := es[key]
	if *got.RevisionID != r.ID || *got.OutputCost != *old.OutputCost+15_000_000 {
		t.Errorf("revisão %d (quer %d); saída %s → %s", *got.RevisionID, r.ID, old.OutputCost, got.OutputCost)
	}
}

func TestCostDisabledRevisionStays(t *testing.T) {
	c := newCostEnv(t)
	ctx := context.Background()
	r := c.add(t, claudeUSD)
	before := c.total(t, allClaude, "USD")
	if err := c.svc.DisablePrice(ctx, r.ID); err != nil {
		t.Fatal(err)
	}
	if got := c.total(t, allClaude, "USD"); got != before {
		t.Errorf("desativar mudou o custo: %s → %s", before, got)
	}
	// Recalcular sem preço vigente: vira sem preço.
	c.recalc(ctx)
	v, _ := c.svc.Costs(ctx, allClaude, "")
	if len(v.Totals) != 0 || v.Quality.NoPrice == 0 {
		t.Errorf("recalculado sem preço: %+v", v)
	}
	// Reativar cria revisão nova e volta a valer.
	nr, err := c.svc.ReactivatePrice(ctx, r.ID)
	if err != nil || nr.ID == r.ID || nr.DisabledAt != nil {
		t.Fatalf("%+v %v", nr, err)
	}
	if got := c.total(t, allClaude, "USD"); got != before {
		t.Errorf("reativado: %s; quer %s", got, before)
	}
}

func TestCostCloseValidityKeepsEstimates(t *testing.T) {
	c := newCostEnv(t)
	ctx := context.Background()
	r := c.add(t, claudeUSD)
	before := c.total(t, allClaude, "USD")
	in := doubled(claudeUSD)
	in.ValidFrom = "2026-11-21" // depois das respostas do Claude (20/11)
	res, err := c.svc.NewPriceFrom(ctx, r.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	nr := res.Revision
	if got := c.total(t, allClaude, "USD"); got != before {
		t.Errorf("custo mudou: %s → %s", before, got)
	}
	items, _ := c.svc.Prices(ctx)
	var oldItem *PriceItem
	for i := range items {
		if items[i].Current.ID == r.ID {
			oldItem = &items[i]
		}
	}
	if oldItem == nil || len(oldItem.History) != 1 || oldItem.Current.ValidTo == nil || oldItem.Current.ClosedAt == nil || nr.PriceKey == r.PriceKey {
		t.Errorf("histórico: %+v", oldItem)
	}
	// Recalcular não muda: as respostas são de antes do novo preço.
	c.recalc(ctx)
	if got := c.total(t, allClaude, "USD"); got != before {
		t.Errorf("recalculado: %s", got)
	}
}

func TestCostRecalculateIncludesDedupCopies(t *testing.T) {
	c := newCostEnv(t)
	ctx := context.Background()
	r := c.add(t, claudeUSD)
	nr, _ := c.svc.EditPrice(ctx, r.ID, doubled(claudeUSD))
	c.recalc(ctx)
	es, _ := c.prices.Estimates(ctx)
	byResp := map[string]int{}
	for k, e := range es {
		if k.Provider != domain.ProviderClaude {
			continue
		}
		byResp[k.ResponseID]++
		if e.RevisionID != nil && *e.RevisionID != nr.ID {
			t.Errorf("%v ficou na revisão %d", k, *e.RevisionID)
		}
	}
	copies := 0
	for _, n := range byResp {
		if n > 1 {
			copies++
		}
	}
	if copies == 0 {
		t.Error("fixture sem resposta copiada")
	}
}

func TestCostsNeverMixCurrencies(t *testing.T) {
	c := newCostEnv(t)
	c.add(t, claudeUSD)
	codex := claudeUSD
	codex.Provider, codex.Model, codex.Currency = domain.ProviderCodex, "gpt-*", "BRL"
	c.add(t, codex)
	all := AnalyticsQuery{Period: "all"}
	v, err := c.svc.Costs(context.Background(), all, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Totals) != 2 {
		t.Fatalf("moedas: %+v", v.Totals)
	}
	only := func(p domain.ProviderID) domain.Nanos {
		return c.total(t, AnalyticsQuery{Period: "all", Providers: []domain.ProviderID{p}}, map[domain.ProviderID]string{domain.ProviderClaude: "USD", domain.ProviderCodex: "BRL"}[p])
	}
	for _, s := range v.Totals {
		want := only(domain.ProviderClaude)
		if s.Currency == "BRL" {
			want = only(domain.ProviderCodex)
		}
		if s.Total != want || want == 0 {
			t.Errorf("%s: %s; quer %s", s.Currency, s.Total, want)
		}
	}
	for _, g := range v.ByProvider {
		if len(g.Sums) > 2 { // uma moeda + linhas sem custo
			t.Errorf("%s com %d moedas", g.Key, len(g.Sums))
		}
	}
}

func TestCostsComparePrevious(t *testing.T) {
	c := newCostEnv(t)
	c.add(t, claudeUSD)
	q := AnalyticsQuery{Period: "custom", From: "2026-11-21", To: "2026-11-21", Providers: []domain.ProviderID{domain.ProviderClaude}}
	v, err := c.svc.Costs(context.Background(), q, "")
	if err != nil {
		t.Fatal(err)
	}
	if !v.Window.HasPrev || len(v.Totals) != 0 || len(v.Prev) != 1 || v.Prev[0].Total != c.total(t, allClaude, "USD") {
		t.Errorf("atual %+v anterior %+v", v.Totals, v.Prev)
	}
	if v, _ := c.svc.Costs(context.Background(), allClaude, ""); v.Window.HasPrev || len(v.Prev) != 0 {
		t.Errorf("all com anterior: %+v", v.Prev)
	}
}

func TestCostsBucketOverride(t *testing.T) {
	c := newCostEnv(t)
	c.add(t, claudeUSD)
	q := AnalyticsQuery{Period: "custom", From: "2026-09-01", To: "2026-11-25", Providers: []domain.ProviderID{domain.ProviderClaude}}
	for _, b := range []string{"day", "week", "month"} {
		v, err := c.svc.Costs(context.Background(), q, b)
		if err != nil {
			t.Fatal(err)
		}
		var sum domain.Nanos
		for _, p := range v.Series {
			sum += p.ByCurrency["USD"]
		}
		if v.Window.Bucket != b || sum != v.Totals[0].Total {
			t.Errorf("%s: intervalo %s, série %s ≠ total %s", b, v.Window.Bucket, sum, v.Totals[0].Total)
		}
		if b == "week" && time.UnixMilli(v.Series[0].Start).Weekday() != time.Monday {
			t.Errorf("semana começa %v", time.UnixMilli(v.Series[0].Start).Weekday())
		}
	}
	if _, err := c.svc.Costs(context.Background(), q, "ano"); !errors.Is(err, ErrInvalidFilter) {
		t.Errorf("intervalo inválido: %v", err)
	}
}

func TestCostSessionsAndUncalculable(t *testing.T) {
	c := newCostEnv(t)
	c.add(t, claudeUSD)
	all := AnalyticsQuery{Period: "all"}
	p, err := c.svc.CostSessions(context.Background(), all, 0)
	if err != nil || p.Total == 0 || p.Limit != usagePageSize {
		t.Fatalf("%+v %v", p, err)
	}
	u, err := c.svc.Uncalculable(context.Background(), all, 0)
	if err != nil || u.Total == 0 {
		t.Fatalf("%+v %v", u, err)
	}
	for _, r := range u.Rows {
		if r.Key.Provider == domain.ProviderClaude {
			t.Errorf("Claude listado sem custo: %+v", r)
		}
	}
}

func TestRefreshSerialized(t *testing.T) {
	c := newCostEnv(t)
	ctx := context.Background()
	r := c.add(t, claudeUSD)
	var wg sync.WaitGroup
	for i := range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 3 {
				switch i % 3 {
				case 0:
					c.svc.Refresh(ctx)
				case 1:
					items, _ := c.svc.Prices(ctx)
					for _, it := range items {
						c.svc.EditPrice(ctx, it.Current.ID, doubled(claudeUSD))
					}
				default:
					c.recalc(ctx)
				}
			}
		}()
	}
	wg.Wait()
	revs, _ := c.prices.Revisions(ctx)
	ids := map[int64]bool{}
	for _, rv := range revs {
		ids[rv.ID] = true
	}
	es, _ := c.prices.Estimates(ctx)
	for k, e := range es {
		if e.RevisionID != nil && !ids[*e.RevisionID] {
			t.Errorf("%v aponta para revisão inexistente %d", k, *e.RevisionID)
		}
	}
	_ = r
}

func TestPriceInputValidation(t *testing.T) {
	c := newCostEnv(t)
	r := c.add(t, claudeUSD)
	if r.Currency != "USD" || *r.CacheRead != "0.3" || r.UnitTokens != 1_000_000 {
		t.Errorf("normalização: %+v", r)
	}
	bad := claudeUSD
	bad.Model, bad.ValidFrom, bad.ValidTo = "outro", "2026-11-10", "2026-11-01"
	if _, err := c.svc.AddPrice(context.Background(), bad); !errors.Is(err, domain.ErrInvalidPrice) {
		t.Errorf("vigência invertida: %v", err)
	}
	dup := claudeUSD
	if _, err := c.svc.AddPrice(context.Background(), dup); !errors.Is(err, domain.ErrInvalidPrice) {
		t.Errorf("sobreposição: %v", err)
	}
	if _, err := c.svc.EditPrice(context.Background(), 999, claudeUSD); !errors.Is(err, domain.ErrInvalidPrice) {
		t.Errorf("inexistente: %v", err)
	}
	// ValidTo é o último dia, inclusivo.
	in := claudeUSD
	in.Model, in.ValidTo = "x", "2026-11-30"
	r2, err := c.svc.AddPrice(context.Background(), in)
	if err != nil || r2.ValidTo == nil || !r2.ValidTo.Equal(time.Date(2026, 12, 1, 0, 0, 0, 0, brt)) {
		t.Errorf("fim %v %v", r2.ValidTo, err)
	}
	if c.svc.DefaultCurrency() != "BRL" || c.svc.SetDefaultCurrency(context.Background(), "eur") != nil || c.svc.DefaultCurrency() != "EUR" {
		t.Errorf("moeda padrão %q", c.svc.DefaultCurrency())
	}
	if !errors.Is(c.svc.SetDefaultCurrency(context.Background(), "real"), domain.ErrInvalidPrice) {
		t.Error("moeda padrão inválida aceita")
	}
}

// Novo preço a partir de uma data: as estimativas já calculadas depois
// dessa data continuam com o preço anterior e a resposta diz quantas são
// (spec §3.1: "a tela avisa quantas são").
func TestNewPriceFromReportsStale(t *testing.T) {
	c := newCostEnv(t)
	ctx := context.Background()
	r := c.add(t, claudeUSD)
	in := doubled(claudeUSD)
	in.ValidFrom = "2026-11-20" // dia das respostas do Claude
	res, err := c.svc.NewPriceFrom(ctx, r.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	es, _ := c.prices.Estimates(ctx)
	from := res.Revision.ValidFrom.UnixMilli()
	want := 0
	for _, e := range es {
		if e.RevisionID != nil && *e.RevisionID == r.ID && e.UsageTs != nil && *e.UsageTs >= from {
			want++
		}
	}
	if want == 0 || res.Stale != want {
		t.Errorf("Stale %d; quer %d", res.Stale, want)
	}
	// Depois das respostas: nenhuma fica para trás.
	r2 := c.add(t, PriceInput{Provider: domain.ProviderCodex, Model: "gpt-*", Currency: "USD", UnitTokens: 1_000_000, Input: ptr("1"), ValidFrom: "2026-01-01"})
	in2 := PriceInput{Provider: domain.ProviderCodex, Model: "gpt-*", Currency: "USD", UnitTokens: 1_000_000, Input: ptr("2"), ValidFrom: "2026-12-01"}
	if res2, err := c.svc.NewPriceFrom(ctx, r2.ID, in2); err != nil || res2.Stale != 0 {
		t.Errorf("%+v %v", res2, err)
	}
}
