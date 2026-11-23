package service

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"sessionflow/internal/domain"
	"sessionflow/internal/indexer/indexertest"
	"sessionflow/internal/repository/sqlite"
)

// TestPrintVerificationTable compara os valores esperados dos fixtures
// (docs/discovery/fixtures/usage/README.md) com os calculados pelo
// indexador + Analytics, e imprime a tabela (go test -v -run Verification).
func TestPrintVerificationTable(t *testing.T) {
	a, stats := analyticsEnv(t)
	ctx := context.Background()
	q := func(p ...domain.ProviderID) AnalyticsQuery { return AnalyticsQuery{Period: "all", Providers: p} }
	usage := func(p ...domain.ProviderID) UsageView {
		v, err := a.Usage(ctx, q(p...))
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	origin := func(v UsageView, o string) int64 {
		for _, g := range v.ByOrigin {
			if g.Key == o {
				return g.Tokens.Total.Sum
			}
		}
		return -1
	}
	cl, cx, oc := usage(domain.ProviderClaude), usage(domain.ProviderCodex), usage(domain.ProviderOpenCode)
	pages, _ := a.UsageSessions(ctx, q(domain.ProviderClaude), 0)
	row := func(id string) sqlite.UsageSessionRow {
		for _, r := range pages.Rows {
			if r.Key.NativeID == id {
				return r
			}
		}
		return sqlite.UsageSessionRow{}
	}
	act, _ := a.Activity(ctx, q(domain.ProviderClaude))
	dash, _ := stats.Dashboard(ctx, StatsQuery{Period: "all", Providers: []domain.ProviderID{domain.ProviderClaude}})
	e := indexertest.NewUsageEnv(t)
	e.Run(t)
	first := e.Count(t, "SELECT count(*) FROM usage")
	e.DB.Exec("DELETE FROM sync_state")
	e.Run(t)
	again := e.Count(t, "SELECT count(*) FROM usage")

	cases := []struct {
		name      string
		want, got any
	}{
		{"Claude: registros após deduplicação (resposta em 3 linhas = 1; cópia no subagente = 1)", 7, cl.Totals.Records},
		{"Claude: total derivado (entrada+saída+cache)", int64(142424), cl.Totals.Total.Sum},
		{"Claude: registros com total (msg_FIX_C sem cache_creation fica sem total)", 6, cl.Totals.Total.Count},
		{"Claude: reasoning indisponível (registros que informam)", 0, cl.Totals.Reasoning.Count},
		{"Claude: toolUseResult.totalTokens (99999) não somado — saída total", int64(1005), cl.Totals.Output.Sum},
		{"Codex: pedidos contados (repetido e info nulo fora)", 3, cx.Totals.Records},
		{"Codex: soma dos pedidos = acumulado final", int64(48017), cx.Totals.Total.Sum},
		{"Codex: entrada sem cache (derivada)", int64(1051 + 4453 + 10985), cx.Totals.Input.Sum},
		{"Codex: reasoning (dentro da saída, não somado ao total)", int64(359), cx.Totals.Reasoning.Sum},
		{"Codex: cache criado indisponível", 0, cx.Totals.CacheCreation.Count},
		{"OpenCode: respostas (step-finish fora)", 3, oc.Totals.Records},
		{"OpenCode: total (informado 11.726 + derivado 5.615; 3ª sem total)", int64(17341), oc.Totals.Total.Sum},
		{"OpenCode: reasoning separado, sem dupla contagem", int64(12), oc.Totals.Reasoning.Sum},
		{"Origem principal (A+B)", int64(129041), origin(cl, "main_agent")},
		{"Origem subagente (D+E+G não associada)", int64(9125 + 1662), origin(cl, "subagent")},
		{"Origem automática (F)", int64(2596), origin(cl, "automatic")},
		{"Sessão principal consolidada (principal + 2 subagentes)", int64(138166), row("33333333-3333-4333-8333-333333333333").Tokens.Total.Sum},
		{"Subordinada sem pai no índice: contabilizada", int64(1662), row("55555555-5555-4555-8555-555555555555/agent-o1").Tokens.Total.Sum},
		{"Subordinada sem pai no índice: marcada não associada", false, row("55555555-5555-4555-8555-555555555555/agent-o1").Associated},
		{"Atividade Claude: sessões (só a principal)", 1, act.Sessions.Value},
		{"Atividade = Dashboard: sessões", dash.Sessions.Value, act.Sessions.Value},
		{"Atividade = Dashboard: mensagens", dash.Messages.Value, act.Messages.Value},
		{"Releitura idempotente: registros na 1ª e na 2ª indexação", first, again},
	}
	var b strings.Builder
	b.WriteString("\n| Caso | Esperado | Calculado | |\n|---|---|---|---|\n")
	for _, c := range cases {
		ok := fmt.Sprint(c.want) == fmt.Sprint(c.got)
		mark := "ok"
		if !ok {
			mark = "DIFERENTE"
			t.Errorf("%s: esperado %v, calculado %v", c.name, c.want, c.got)
		}
		fmt.Fprintf(&b, "| %s | %v | %v | %s |\n", c.name, c.want, c.got, mark)
	}
	t.Log(b.String())
}
