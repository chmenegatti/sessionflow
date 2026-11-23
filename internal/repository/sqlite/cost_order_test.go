package sqlite

import (
	"math/rand/v2"
	"slices"
	"testing"

	"sessionflow/internal/domain"
)

// Ordenação de custos com moedas diferentes (hardening, item 3).

type orow struct {
	name string
	sums []CostSums
}

func cs(cur string, total domain.Nanos, records int) CostSums {
	return CostSums{Currency: cur, Total: total, Records: records}
}

func sortRows(rows []orow) []string {
	slices.SortFunc(rows, func(a, b orow) int {
		if c := CompareCostSums(a.sums, b.sums); c != 0 {
			return c
		}
		if a.name < b.name {
			return -1
		}
		if a.name > b.name {
			return 1
		}
		return 0
	})
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.name
	}
	return out
}

func TestCompareCostSumsByCurrency(t *testing.T) {
	rows := []orow{
		{"usd-grande", []CostSums{cs("USD", 9_000, 1)}},
		{"brl-pequeno", []CostSums{cs("BRL", 10, 1)}},
		{"eur-medio", []CostSums{cs("EUR", 500, 1)}},
		{"brl-grande", []CostSums{cs("BRL", 1_000, 1)}},
		{"usd-pequeno", []CostSums{cs("USD", 1, 1)}},
		{"eur-negativo", []CostSums{cs("EUR", -5, 1)}},
		// BRL e USD na mesma linha: entra no ranking pela primeira moeda
		// (BRL), comparada só com BRL.
		{"misto", []CostSums{cs("USD", 1_000_000, 1), cs("BRL", 100, 1)}},
		{"sem-preco-muitas", []CostSums{cs("", 0, 9)}},
		{"sem-preco-poucas", []CostSums{cs("", 0, 2)}},
		{"vazio", nil},
	}
	want := []string{
		"brl-grande", "misto", "brl-pequeno", // BRL, do maior para o menor
		"eur-medio", "eur-negativo", // EUR
		"usd-grande", "usd-pequeno", // USD
		"sem-preco-muitas", "sem-preco-poucas", "vazio", // sem custo: por respostas
	}
	if got := sortRows(slices.Clone(rows)); !slices.Equal(got, want) {
		t.Errorf("ordem\n got %v\nwant %v", got, want)
	}
	// Determinística: qualquer ordem de entrada dá o mesmo resultado.
	r := rand.New(rand.NewPCG(1, 2))
	for range 50 {
		in := slices.Clone(rows)
		r.Shuffle(len(in), func(i, j int) { in[i], in[j] = in[j], in[i] })
		if got := sortRows(in); !slices.Equal(got, want) {
			t.Fatalf("ordem mudou com a entrada embaralhada: %v", got)
		}
	}
}

func TestCompareCostSumsTies(t *testing.T) {
	a := []CostSums{cs("USD", 100, 3)}
	b := []CostSums{cs("USD", 100, 5)}
	if CompareCostSums(a, b) <= 0 {
		t.Error("mesmo total: mais respostas deveria vir antes")
	}
	if c := CompareCostSums(a, []CostSums{cs("USD", 100, 3)}); c != 0 {
		t.Errorf("empate total: %d", c)
	}
	// Nunca compara valor entre moedas: USD gigante não passa BRL mínimo.
	if CompareCostSums([]CostSums{cs("BRL", 1, 1)}, []CostSums{cs("USD", 1<<60, 1)}) >= 0 {
		t.Error("ordem entre moedas decidida pelo valor")
	}
}
