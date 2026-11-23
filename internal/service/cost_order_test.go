package service

import (
	"slices"
	"testing"

	"sessionflow/internal/domain"
	"sessionflow/internal/repository/sqlite"
)

// groupCosts nunca ordena somando moedas (hardening, item 3).
func TestGroupCostsSeparatesCurrencies(t *testing.T) {
	row := func(model, cur string, total domain.Nanos, n int) sqlite.CostRow {
		return sqlite.CostRow{Provider: domain.ProviderClaude, Model: model, Sums: sqlite.CostSums{Currency: cur, Total: total, Records: n}}
	}
	rows := []sqlite.CostRow{
		row("usd-grande", "USD", 9_000_000, 1),
		row("brl-pequeno", "BRL", 10, 1),
		row("eur", "EUR", 50, 1),
		row("brl-grande", "BRL", 1_000, 1),
		row("sem-preco", "", 0, 7),
		row("usd-empate-a", "USD", 5, 2),
		row("usd-empate-b", "USD", 5, 2),
	}
	got := []string{}
	for _, g := range groupCosts(rows, func(r sqlite.CostRow) (string, string) { return r.Model, r.Model }, 100) {
		got = append(got, g.Key)
	}
	want := []string{"brl-grande", "brl-pequeno", "eur", "usd-grande", "usd-empate-a", "usd-empate-b", "sem-preco"}
	if !slices.Equal(got, want) {
		t.Errorf("ordem\n got %v\nwant %v", got, want)
	}
}

// O limite vale por moeda: muitos grupos em BRL não escondem os de USD.
func TestGroupCostsLimitPerCurrency(t *testing.T) {
	var rows []sqlite.CostRow
	for i, m := range []string{"b1", "b2", "b3"} {
		rows = append(rows, sqlite.CostRow{Model: m, Sums: sqlite.CostSums{Currency: "BRL", Total: domain.Nanos(100 - i), Records: 1}})
	}
	rows = append(rows, sqlite.CostRow{Model: "u1", Sums: sqlite.CostSums{Currency: "USD", Total: 1, Records: 1}},
		sqlite.CostRow{Model: "x", Sums: sqlite.CostSums{Records: 1}})
	got := []string{}
	for _, g := range groupCosts(rows, func(r sqlite.CostRow) (string, string) { return r.Model, r.Model }, 2) {
		got = append(got, g.Key)
	}
	if want := []string{"b1", "b2", "u1", "x"}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// Grupo com duas moedas entra se estiver no top N de qualquer uma delas.
func TestGroupCostsLimitMixedGroup(t *testing.T) {
	var rows []sqlite.CostRow
	for i, m := range []string{"b1", "b2"} {
		rows = append(rows, sqlite.CostRow{Model: m, Sums: sqlite.CostSums{Currency: "BRL", Total: domain.Nanos(1000 - i), Records: 1}})
	}
	rows = append(rows,
		sqlite.CostRow{Model: "misto", Sums: sqlite.CostSums{Currency: "BRL", Total: 1, Records: 1}},
		sqlite.CostRow{Model: "misto", Sums: sqlite.CostSums{Currency: "USD", Total: 10_000, Records: 1}},
		sqlite.CostRow{Model: "u1", Sums: sqlite.CostSums{Currency: "USD", Total: 5, Records: 1}},
		sqlite.CostRow{Model: "u2", Sums: sqlite.CostSums{Currency: "USD", Total: 4, Records: 1}})
	got := []string{}
	for _, g := range groupCosts(rows, func(r sqlite.CostRow) (string, string) { return r.Model, r.Model }, 2) {
		got = append(got, g.Key)
	}
	// misto: fora do top 2 em BRL, mas o maior em USD; u2 fica de fora.
	if want := []string{"b1", "b2", "misto", "u1"}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}
