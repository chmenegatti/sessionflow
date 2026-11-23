package sqlite_test

import (
	"context"
	"testing"
	"time"

	"sessionflow/internal/domain"
	"sessionflow/internal/indexer/indexertest"
	"sessionflow/internal/repository/sqlite"
)

// Valores esperados: docs/discovery/fixtures/usage/README.md.
func usageIndex(t *testing.T) *sqlite.IndexStore {
	t.Helper()
	e := indexertest.NewUsageEnv(t)
	e.Run(t)
	return e.Store
}

var year = sqlite.UsageFilter{
	From: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), Bucket: sqlite.BucketDay,
}

func only(p domain.ProviderID) sqlite.UsageFilter {
	f := year
	f.Providers = []domain.ProviderID{p}
	return f
}

func field(t *testing.T, name string, got sqlite.Field, sum int64, count int) {
	t.Helper()
	if got.Sum != sum || got.Count != count {
		t.Errorf("%s = %d (%d registros); quer %d (%d)", name, got.Sum, got.Count, sum, count)
	}
}

func TestUsageTotalsAllOrigins(t *testing.T) {
	s := usageIndex(t)
	c, err := s.UsageTotals(context.Background(), only(domain.ProviderClaude))
	if err != nil {
		t.Fatal(err)
	}
	if c.Records != 7 {
		t.Errorf("Claude: %d registros; quer 7 (msg_FIX_B uma vez)", c.Records)
	}
	field(t, "entrada", c.Input, 20, 7)
	field(t, "saída", c.Output, 1005, 7)
	field(t, "cache lido", c.CacheRead, 131404, 7)
	field(t, "cache criado", c.CacheCreation, 11048, 6)
	field(t, "total", c.Total, 142424, 6)
	field(t, "reasoning", c.Reasoning, 0, 0)
	if c.Derived != 6 {
		t.Errorf("totais derivados: %d", c.Derived)
	}
	x, _ := s.UsageTotals(context.Background(), only(domain.ProviderCodex))
	field(t, "codex total", x.Total, 48017, 3)
	field(t, "codex cache criado", x.CacheCreation, 0, 0)
	field(t, "codex reasoning", x.Reasoning, 359, 3)
	o, _ := s.UsageTotals(context.Background(), only(domain.ProviderOpenCode))
	field(t, "opencode total", o.Total, 17341, 2)
	field(t, "opencode reasoning", o.Reasoning, 12, 3)
}

func TestUsageCrossFileDedupPrefersMain(t *testing.T) {
	s := usageIndex(t)
	gs, err := s.UsageBy(context.Background(), only(domain.ProviderClaude), sqlite.UsageByOrigin, 10)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int64{}
	for _, g := range gs {
		got[g.Key] = g.Tokens.Total.Sum
	}
	// main: A + B (C sem total); subagent: D + E + G (órfão); automatic: F.
	if got["main_agent"] != 129041 || got["subagent"] != 9125+1662 || got["automatic"] != 2596 {
		t.Errorf("por origem: %v", got)
	}
}

func TestUsageByProviderModelProject(t *testing.T) {
	s := usageIndex(t)
	ctx := context.Background()
	prov, _ := s.UsageBy(ctx, year, sqlite.UsageByProvider, 10)
	want := map[string]int64{"claude": 142424, "codex": 48017, "opencode": 17341}
	if len(prov) != 3 {
		t.Fatalf("%+v", prov)
	}
	for _, g := range prov {
		if g.Tokens.Total.Sum != want[g.Key] {
			t.Errorf("provider %s: %d", g.Key, g.Tokens.Total.Sum)
		}
	}
	if prov[0].Key != "claude" {
		t.Errorf("ordem: %s primeiro", prov[0].Key)
	}
	models, _ := s.UsageBy(ctx, year, sqlite.UsageByModel, 10)
	m := map[string]int64{}
	for _, g := range models {
		m[g.Key] = g.Tokens.Total.Sum
	}
	if m["claude-opus-5-5"] != 142424 || m["gpt-5.4"] != 48017 || m["claude-opus-4.6"] != 17341 {
		t.Errorf("por modelo: %v", m)
	}
	projects, _ := s.UsageBy(ctx, year, sqlite.UsageByProject, 10)
	if len(projects) != 1 || projects[0].Key != "/home/user/projects/demo-app" || projects[0].Label != "demo-app" ||
		projects[0].Tokens.Total.Sum != 142424+48017+17341 {
		t.Errorf("por projeto: %+v", projects)
	}
	f := year
	f.Model = "gpt-5.4"
	if tot, _ := s.UsageTotals(ctx, f); tot.Records != 4 { // 3 Codex + 1 OpenCode
		t.Errorf("filtro de modelo: %d registros", tot.Records)
	}
}

func TestUsageSessionsRollup(t *testing.T) {
	s := usageIndex(t)
	rows, total, err := s.UsageSessions(context.Background(), only(domain.ProviderClaude), 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || len(rows) != 3 {
		t.Fatalf("%d raízes (quer 3: principal, automática, órfão): %+v", total, rows)
	}
	byKey := map[string]sqlite.UsageSessionRow{}
	for _, r := range rows {
		byKey[r.Key.NativeID] = r
	}
	main := byKey["33333333-3333-4333-8333-333333333333"]
	if main.Tokens.Total.Sum != 138166 || main.Tokens.Records != 5 || main.Origin != "main_agent" || !main.Associated {
		t.Errorf("principal: %+v", main)
	}
	orphan := byKey["55555555-5555-4555-8555-555555555555/agent-o1"]
	if orphan.Associated || orphan.Origin != "subagent" || orphan.Tokens.Total.Sum != 1662 {
		t.Errorf("órfão: %+v", orphan)
	}
	auto := byKey["44444444-4444-4444-8444-444444444444"]
	if auto.Origin != "automatic" || auto.Tokens.Total.Sum != 2596 {
		t.Errorf("automática: %+v", auto)
	}
	if rows[0].Key.NativeID != main.Key.NativeID || main.FirstTs == nil || main.LastTs == nil || *main.LastTs <= *main.FirstTs {
		t.Errorf("ordem/duração: %+v", rows[0])
	}
	page, total, _ := s.UsageSessions(context.Background(), only(domain.ProviderClaude), 2, 50)
	if total != 3 || len(page) != 1 {
		t.Errorf("paginação: %d de %d", len(page), total)
	}
}

func TestUsageOriginsReconcile(t *testing.T) {
	s := usageIndex(t)
	ctx := context.Background()
	rows, _, _ := s.UsageSessions(ctx, only(domain.ProviderClaude), 0, 50)
	for _, r := range rows {
		parts, err := s.UsageSessionOrigins(ctx, only(domain.ProviderClaude), r.SessionID)
		if err != nil {
			t.Fatal(err)
		}
		var sum int64
		var n int
		for _, p := range parts {
			sum += p.Tokens.Total.Sum
			n += p.Tokens.Records
		}
		if sum != r.Tokens.Total.Sum || n != r.Tokens.Records {
			t.Errorf("%s: origens %d/%d ≠ linha %d/%d", r.Key.NativeID, sum, n, r.Tokens.Total.Sum, r.Tokens.Records)
		}
	}
	all, _ := s.UsageTotals(ctx, year)
	byOrigin, _ := s.UsageBy(ctx, year, sqlite.UsageByOrigin, 10)
	var sum int64
	for _, g := range byOrigin {
		sum += g.Tokens.Total.Sum
	}
	if sum != all.Total.Sum {
		t.Errorf("por origem %d ≠ total %d", sum, all.Total.Sum)
	}
}

func TestUsageSeriesLocalDay(t *testing.T) {
	s := usageIndex(t)
	f := only(domain.ProviderCodex)
	f.Offset = -3 * 3600
	pts, err := s.UsageSeries(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	// Pedidos do Codex às 15:0x UTC = 12:0x em -03:00, dia 20.
	if len(pts) != 1 || pts[0].Bucket != "2026-11-20" || pts[0].Total.Sum != 48017 {
		t.Errorf("%+v", pts)
	}
}

func TestUsageNullNotZero(t *testing.T) {
	s := usageIndex(t)
	f := year
	f.Providers = []domain.ProviderID{domain.ProviderClaude}
	tot, _ := s.UsageTotals(context.Background(), f)
	if tot.Reasoning.Count != 0 || tot.CacheCreation.Count == tot.Records {
		t.Errorf("cobertura: reasoning %d, cache criado %d de %d", tot.Reasoning.Count, tot.CacheCreation.Count, tot.Records)
	}
	empty := year
	empty.Project = "/nao/existe"
	z, err := s.UsageTotals(context.Background(), empty)
	if err != nil || z.Records != 0 || z.Total.Count != 0 {
		t.Errorf("seleção vazia: %+v %v", z, err)
	}
}

func TestUsageModels(t *testing.T) {
	s := usageIndex(t)
	ms, err := s.UsageModels(context.Background())
	if err != nil || len(ms) != 3 || ms[0] != "claude-opus-4.6" {
		t.Errorf("%v %v", ms, err)
	}
}

func TestUsageFirst(t *testing.T) {
	s := usageIndex(t)
	first, err := s.UsageFirst(context.Background(), []domain.ProviderID{domain.ProviderCodex})
	if err != nil || !first.Equal(time.Date(2026, 11, 20, 15, 3, 0, 0, time.UTC)) {
		t.Errorf("%v %v", first, err)
	}
}

// UsageBreakdown + Group (uma consulta, agregada em Go) = UsageBy (uma
// consulta por dimensão), em valores e ordem.
func TestUsageBreakdownMatchesUsageBy(t *testing.T) {
	s := usageIndex(t)
	ctx := context.Background()
	rows, err := s.UsageBreakdown(ctx, year)
	if err != nil {
		t.Fatal(err)
	}
	for _, dim := range []sqlite.UsageDim{sqlite.UsageByProvider, sqlite.UsageByModel, sqlite.UsageByProject, sqlite.UsageByOrigin} {
		want, _ := s.UsageBy(ctx, year, dim, 50)
		got := sqlite.Group(rows, dim, 50)
		if len(got) != len(want) {
			t.Fatalf("%s: %d grupos; quer %d", dim, len(got), len(want))
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("%s[%d] = %+v; quer %+v", dim, i, got[i], want[i])
			}
		}
	}
	tot, _ := s.UsageTotals(ctx, year)
	if sum := sqlite.Sum(rows); sum != tot {
		t.Errorf("soma %+v ≠ totais %+v", sum, tot)
	}
}

// Subagente dentro de subagente (OpenCode permite): consolida na principal e
// não é "não associado".
func TestUsageSessionsNestedRoot(t *testing.T) {
	e := indexertest.NewUsageEnv(t)
	ctx := context.Background()
	key := func(id string) domain.SessionKey {
		return domain.SessionKey{Provider: domain.ProviderOpenCode, NativeID: id}
	}
	tx, err := e.Store.BeginSource(ctx, domain.ProviderOpenCode, "/oc.db", domain.SourceSQLite)
	if err != nil {
		t.Fatal(err)
	}
	ts := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	total := func(n int64) *int64 { return &n }
	for i, s := range []struct{ id, parent string }{{"ses_main", ""}, {"ses_a", "ses_main"}, {"ses_b", "ses_a"}} {
		tx.AddUsage(ctx, key(s.id), domain.Usage{ResponseID: "r" + s.id, Timestamp: &ts, Total: total(int64(100 * (i + 1))), Status: domain.UsageStatusComplete})
		sess := domain.Session{Key: key(s.id), Title: s.id}
		if s.parent != "" {
			p := key(s.parent)
			sess.Parent = &p
		}
		tx.PutSession(ctx, sess)
	}
	if err := tx.Commit(ctx, sqlite.SyncState{Status: "ok"}); err != nil {
		t.Fatal(err)
	}
	rows, total2, err := e.Store.UsageSessions(ctx, only(domain.ProviderOpenCode), 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	if total2 != 1 || len(rows) != 1 || rows[0].Key.NativeID != "ses_main" || rows[0].Tokens.Total.Sum != 600 || !rows[0].Associated {
		t.Errorf("%d raízes: %+v", total2, rows)
	}
}
