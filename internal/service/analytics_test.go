package service

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"sessionflow/internal/domain"
	"sessionflow/internal/indexer/indexertest"
	"sessionflow/internal/repository/sqlite"
)

// analyticsEnv: índice com os fixtures de consumo (docs/discovery/fixtures/usage).
func analyticsEnv(t *testing.T) (*AnalyticsService, *StatsService) {
	t.Helper()
	e := indexertest.NewUsageEnv(t)
	e.Run(t)
	udb, err := sqlite.OpenUser(context.Background(), filepath.Join(t.TempDir(), "user.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { udb.Close() })
	user := sqlite.NewUserStore(udb)
	stats := NewStatsService(e.Store, NewSessionService(e.Store, user, discard), user, discard)
	stats.now = func() time.Time { return statNow }
	a := NewAnalyticsService(e.Store, stats, discard)
	a.now = func() time.Time { return statNow }
	return a, stats
}

var allClaude = AnalyticsQuery{Period: "all", Providers: []domain.ProviderID{domain.ProviderClaude}}

// (a) Consumo inclui principal, subagentes, automática e não associada.
func TestDomainRuleConsumptionIncludesAllOrigins(t *testing.T) {
	a, _ := analyticsEnv(t)
	v, err := a.Usage(context.Background(), allClaude)
	if err != nil {
		t.Fatal(err)
	}
	if v.Totals.Total.Sum != 142424 || v.Totals.Records != 7 {
		t.Errorf("total %d (%d registros); quer 142424 (7)", v.Totals.Total.Sum, v.Totals.Records)
	}
	origins := map[string]bool{}
	for _, g := range v.ByOrigin {
		origins[g.Key] = true
	}
	if !origins["main_agent"] || !origins["subagent"] || !origins["automatic"] {
		t.Errorf("origens: %v", origins)
	}
}

// (b) A principal consolida os descendentes associados.
func TestDomainRuleMainConsolidatesDescendants(t *testing.T) {
	a, _ := analyticsEnv(t)
	p, err := a.UsageSessions(context.Background(), allClaude, 0)
	if err != nil {
		t.Fatal(err)
	}
	if p.Total != 3 || p.Rows[0].Key.NativeID != "33333333-3333-4333-8333-333333333333" || p.Rows[0].Tokens.Total.Sum != 138166 {
		t.Errorf("%+v", p)
	}
}

// (c) O detalhamento por origem reconcilia com o total.
func TestDomainRuleOriginsReconcile(t *testing.T) {
	a, _ := analyticsEnv(t)
	ctx := context.Background()
	p, _ := a.UsageSessions(ctx, allClaude, 0)
	var grand int64
	for _, r := range p.Rows {
		parts, err := a.SessionOrigins(ctx, allClaude, r.SessionID)
		if err != nil {
			t.Fatal(err)
		}
		var sum int64
		for _, o := range parts {
			sum += o.Tokens.Total.Sum
		}
		if sum != r.Tokens.Total.Sum {
			t.Errorf("%s: %d ≠ %d", r.Key.NativeID, sum, r.Tokens.Total.Sum)
		}
		grand += sum
	}
	v, _ := a.Usage(ctx, allClaude)
	if grand != v.Totals.Total.Sum {
		t.Errorf("soma das sessões %d ≠ total %d", grand, v.Totals.Total.Sum)
	}
}

// (d) Subordinada sem a sessão pai no índice continua contabilizada e marcada.
func TestDomainRuleUnassociatedCounted(t *testing.T) {
	a, _ := analyticsEnv(t)
	p, _ := a.UsageSessions(context.Background(), allClaude, 0)
	for _, r := range p.Rows {
		if r.Key.NativeID == "55555555-5555-4555-8555-555555555555/agent-o1" {
			if r.Associated || r.Tokens.Total.Sum != 1662 {
				t.Errorf("%+v", r)
			}
			return
		}
	}
	t.Error("não associada ausente")
}

// (e) Atividade exclui subagentes e automáticas.
func TestDomainRuleActivityExcludesHidden(t *testing.T) {
	a, _ := analyticsEnv(t)
	v, err := a.Activity(context.Background(), allClaude)
	if err != nil {
		t.Fatal(err)
	}
	// Claude: só a principal 3333 (subagentes, automática e órfão fora);
	// mensagens com texto da principal (regra do Dashboard): 2 pedidos do
	// usuário + respostas com texto A, C e a <synthetic> (tem texto; não tem
	// consumo, mas conta como mensagem).
	if v.Sessions.Value != 1 || v.Messages.Value != 5 {
		t.Errorf("sessões %d, mensagens %d", v.Sessions.Value, v.Messages.Value)
	}
}

// (f) Atividade = Dashboard para os mesmos filtros.
func TestActivityMatchesDashboard(t *testing.T) {
	a, stats := analyticsEnv(t)
	ctx := context.Background()
	for _, period := range []string{"7d", "30d", "90d", "all"} {
		for _, provs := range [][]domain.ProviderID{nil, {domain.ProviderClaude}, {domain.ProviderCodex, domain.ProviderOpenCode}} {
			d, err := stats.Dashboard(ctx, StatsQuery{Period: period, Providers: provs})
			if err != nil {
				t.Fatal(err)
			}
			v, err := a.Activity(ctx, AnalyticsQuery{Period: period, Providers: provs})
			if err != nil {
				t.Fatal(err)
			}
			if v.Sessions != d.Sessions || v.Messages != d.Messages || len(v.SessionsSeries) != len(d.SessionsSeries) {
				t.Errorf("%s %v: atividade %+v/%+v ≠ dashboard %+v/%+v", period, provs, v.Sessions, v.Messages, d.Sessions, d.Messages)
				continue
			}
			for i := range v.SessionsSeries {
				if v.SessionsSeries[i].Total != d.SessionsSeries[i].Total || v.MessagesSeries[i].Total != d.MessagesSeries[i].Total {
					t.Errorf("%s %v: intervalo %d difere", period, provs, i)
				}
			}
		}
	}
}

func TestAnalyticsUnavailableNotZero(t *testing.T) {
	a, _ := analyticsEnv(t)
	v, err := a.Overview(context.Background(), allClaude)
	if err != nil {
		t.Fatal(err)
	}
	if v.Totals.Reasoning.Count != 0 {
		t.Errorf("reasoning com %d registros", v.Totals.Reasoning.Count)
	}
	found := false
	for _, u := range v.Unavailable {
		if u.Provider == domain.ProviderClaude && u.Field == domain.UsageReasoning && u.Reason != "" {
			found = true
		}
		if u.Provider == domain.ProviderCodex {
			t.Errorf("Codex fora do filtro listado: %+v", u)
		}
	}
	if !found {
		t.Errorf("reasoning do Claude não listado como indisponível: %+v", v.Unavailable)
	}
}

func TestAnalyticsEmptySelection(t *testing.T) {
	a, _ := analyticsEnv(t)
	ctx := context.Background()
	q := AnalyticsQuery{Period: "30d", Project: "/nao/existe"}
	o, err := a.Overview(ctx, q)
	if err != nil || o.Totals.Records != 0 || len(o.Series) != 30 {
		t.Errorf("overview: %+v %v", o.Totals, err)
	}
	for _, p := range o.Series {
		if p.Total != 0 || p.ByProvider == nil {
			t.Errorf("ponto %+v", p)
		}
	}
	act, err := a.Activity(ctx, q)
	if err != nil || act.Sessions.Value != 0 || act.ActiveDays != 0 || act.Days != 30 {
		t.Errorf("atividade: %+v %v", act, err)
	}
}

func TestAnalyticsInvalidFilter(t *testing.T) {
	a, _ := analyticsEnv(t)
	for _, c := range []struct {
		q    AnalyticsQuery
		want error
	}{
		{AnalyticsQuery{Period: "custom", From: "2026-11-10", To: "2026-11-01"}, ErrInvalidPeriod},
		{AnalyticsQuery{Period: "7d", Providers: []domain.ProviderID{"gemini"}}, ErrInvalidFilter},
	} {
		if _, err := a.Overview(context.Background(), c.q); !errors.Is(err, c.want) {
			t.Errorf("%+v: %v; quer %v", c.q, err, c.want)
		}
	}
}

func TestActivityHeatmapLocal(t *testing.T) {
	e := newStatsEnv(t)
	e.sess(t, ck2("a"), "/p", []time.Time{time.Date(2026, 11, 16, 23, 30, 0, 0, brt)}) // segunda
	a := NewAnalyticsService(e.store, e.svc, discard)
	a.now = func() time.Time { return statNow }
	v, err := a.Activity(context.Background(), AnalyticsQuery{Period: "30d"})
	if err != nil {
		t.Fatal(err)
	}
	if v.Heatmap[0][23] != 1 || v.ActiveDays != 1 {
		t.Errorf("seg 23h = %d, dias ativos %d", v.Heatmap[0][23], v.ActiveDays)
	}
}

func TestUsageSessionsPageModelFilter(t *testing.T) {
	a, _ := analyticsEnv(t)
	ms, err := a.Models(context.Background())
	if err != nil || len(ms) != 3 {
		t.Fatalf("%v %v", ms, err)
	}
	v, _ := a.Usage(context.Background(), AnalyticsQuery{Period: "all", Model: "gpt-5.4"})
	if v.Totals.Records != 4 {
		t.Errorf("filtro de modelo: %d registros", v.Totals.Records)
	}
}

// Avisos de campo indisponível só para agentes com consumo no recorte.
func TestAnalyticsUnavailableOnlyPresentProviders(t *testing.T) {
	a, _ := analyticsEnv(t)
	// Todos os fixtures são do mesmo projeto: o filtro de modelo deixa só o
	// Claude no recorte.
	v, err := a.Usage(context.Background(), AnalyticsQuery{Period: "all", Model: "claude-opus-5-5"})
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range v.Unavailable {
		if u.Provider != domain.ProviderClaude {
			t.Errorf("aviso de agente sem consumo no recorte: %+v", u)
		}
	}
	if len(v.Unavailable) == 0 {
		t.Error("aviso do Claude ausente")
	}
}

// Intervalo cujos registros não informam o total não pode parecer "zero":
// cada ponto leva quantos registros tem e quantos informam o total.
func TestOverviewSeriesCoverage(t *testing.T) {
	a, _ := analyticsEnv(t)
	o, err := a.Overview(context.Background(), AnalyticsQuery{Period: "custom", From: "2026-11-25", To: "2026-11-25",
		Providers: []domain.ProviderID{domain.ProviderOpenCode}})
	if err != nil {
		t.Fatal(err)
	}
	var recs, covered int
	for _, p := range o.Series {
		recs += p.Records
		covered += p.Covered
	}
	// OpenCode: 3 respostas, 2 com total.
	if recs != 3 || covered != 2 {
		t.Errorf("registros %d, com total %d; quer 3 e 2", recs, covered)
	}
}
