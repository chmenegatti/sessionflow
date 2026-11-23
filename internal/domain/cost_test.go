package domain

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"
)

func i64(v int64) *int64   { return &v }
func str(v string) *string { return &v }
func nanosOf(n *Nanos) int64 {
	if n == nil {
		return -1
	}
	return int64(*n)
}

var t0 = time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

func rev(unit int64, in, out, cr, cc *string) *PriceRevision {
	return &PriceRevision{ID: 1, Provider: ProviderClaude, Model: "claude-opus-5-5", Currency: "USD", UnitTokens: unit,
		Input: in, Output: out, CacheRead: cr, CacheCreation: cc, ValidFrom: t0}
}

func toks(in, out, cr, cc *int64) UsageTokens {
	return UsageTokens{Input: in, Output: out, CacheRead: cr, CacheCreation: cc}
}

func TestComputeEstimateFormula(t *testing.T) {
	r := rev(1_000_000, str("3.00"), str("15.00"), str("0.30"), str("3.75"))
	e := ComputeEstimate(r, ProviderClaude, "claude-opus-5-5", true, toks(i64(1000), i64(500), i64(200000), i64(10000)))
	got := []int64{nanosOf(e.InputCost), nanosOf(e.OutputCost), nanosOf(e.CacheReadCost), nanosOf(e.CacheCreationCost), nanosOf(e.Total)}
	want := []int64{3_000_000, 7_500_000, 60_000_000, 37_500_000, 108_000_000}
	if !slices.Equal(got, want) {
		t.Errorf("custos %v; quer %v", got, want)
	}
	if e.Total.String() != "0.108000000" || e.Status != EstimateComplete || e.Currency != "USD" || *e.RevisionID != 1 {
		t.Errorf("%+v (%s)", e, e.Total)
	}
}

func TestComputeEstimateUnits(t *testing.T) {
	u := toks(i64(1000), i64(500), i64(200000), i64(10000))
	a := ComputeEstimate(rev(1_000_000, str("3"), str("15"), str("0.3"), str("3.75")), ProviderClaude, "m", true, u)
	b := ComputeEstimate(rev(1000, str("0.003"), str("0.015"), str("0.0003"), str("0.00375")), ProviderClaude, "m", true, u)
	if nanosOf(a.Total) != nanosOf(b.Total) {
		t.Errorf("milhão %s ≠ mil %s", a.Total, b.Total)
	}
	d := ComputeEstimate(rev(1, str("0.000003"), str("0.000015"), nil, nil), ProviderClaude, "m", true, toks(i64(1000), i64(500), i64(0), i64(0)))
	if nanosOf(d.Total) != 10_500_000 {
		t.Errorf("por token: %s", d.Total)
	}
}

func TestComputeEstimateNoFloat(t *testing.T) {
	e := ComputeEstimate(rev(1, str("0.1"), str("0.2"), nil, nil), ProviderCodex, "gpt", true, toks(i64(1), i64(1), i64(0), nil))
	if nanosOf(e.Total) != 300_000_000 || e.Total.String() != "0.300000000" {
		t.Errorf("0,1 + 0,2 = %s", e.Total)
	}
}

func TestComputeEstimateHalfEven(t *testing.T) {
	for tokens, want := range map[int64]int64{500: 0, 1500: 2, 2500: 2, 3500: 4} {
		e := ComputeEstimate(rev(1_000_000, str("0.000001"), nil, nil, nil), ProviderCodex, "gpt", true, toks(i64(tokens), i64(0), i64(0), nil))
		if nanosOf(e.InputCost) != want {
			t.Errorf("%d tokens: %d nanos; quer %d", tokens, nanosOf(e.InputCost), want)
		}
	}
}

func TestComputeEstimateOverflow(t *testing.T) {
	e := ComputeEstimate(rev(1, str("1000000"), nil, nil, nil), ProviderCodex, "gpt", true, toks(i64(1_000_000_000_000), i64(0), i64(0), nil))
	if e.Status != EstimateInsufficient || !slices.Contains(e.Reasons, ReasonOverflow) || e.Total != nil || e.InputCost != nil {
		t.Errorf("%+v", e)
	}
}

func TestComputeEstimateStatus(t *testing.T) {
	full := rev(1_000_000, str("3"), str("15"), str("0.3"), str("3.75"))
	noCC := rev(1_000_000, str("3"), str("15"), str("0.3"), nil)
	for _, c := range []struct {
		name   string
		rev    *PriceRevision
		p      ProviderID
		model  string
		hasTs  bool
		u      UsageTokens
		status EstimateStatus
		reason string
	}{
		{"cc sem preço com tokens", noCC, ProviderClaude, "m", true, toks(i64(10), i64(10), i64(10), i64(10000)), EstimatePartial, "cache criado sem preço"},
		{"cc sem preço, 0 tokens", noCC, ProviderClaude, "m", true, toks(i64(10), i64(10), i64(10), i64(0)), EstimateComplete, ""},
		{"codex sem cc", noCC, ProviderCodex, "m", true, toks(i64(10), i64(10), i64(10), nil), EstimateComplete, ""},
		{"claude cc indisponível", full, ProviderClaude, "m", true, toks(i64(10), i64(10), i64(10), nil), EstimatePartial, "cache criado indisponível"},
		{"sem preço", nil, ProviderClaude, "m", true, toks(i64(10), i64(10), i64(10), i64(10)), EstimateNoPrice, ReasonNoPrice},
		{"sem horário", full, ProviderClaude, "m", false, toks(i64(10), i64(10), i64(10), i64(10)), EstimateInsufficient, ReasonNoTime},
		{"sem modelo", full, ProviderClaude, "", true, toks(i64(10), i64(10), i64(10), i64(10)), EstimateInsufficient, ReasonNoModel},
		{"sem tokens", full, ProviderClaude, "m", true, toks(nil, nil, nil, nil), EstimateInsufficient, ReasonNoTokens},
		{"preço zero", rev(1_000_000, str("0"), str("0"), str("0"), str("0")), ProviderClaude, "m", true, toks(i64(10), i64(10), i64(10), i64(10)), EstimateComplete, ""},
	} {
		e := ComputeEstimate(c.rev, c.p, c.model, c.hasTs, c.u)
		if e.Status != c.status || (c.reason != "" && !slices.Contains(e.Reasons, c.reason)) || (c.reason == "" && len(e.Reasons) > 0) {
			t.Errorf("%s: %s %v", c.name, e.Status, e.Reasons)
		}
		switch c.status {
		case EstimateNoPrice, EstimateInsufficient:
			if e.Total != nil || e.InputCost != nil {
				t.Errorf("%s: custo %v", c.name, e.Total)
			}
		case EstimatePartial:
			if e.InputCost == nil || e.Total == nil {
				t.Errorf("%s: entrada não calculada", c.name)
			}
		}
		if c.name == "preço zero" && nanosOf(e.Total) != 0 {
			t.Errorf("preço zero: %s", e.Total)
		}
	}
}

func revFor(id int64, p ProviderID, model string, from time.Time, to *time.Time) PriceRevision {
	return PriceRevision{ID: id, Provider: p, Model: model, Currency: "USD", UnitTokens: 1_000_000, Input: str("1"), ValidFrom: from, ValidTo: to}
}

func TestMatchPricePrecedence(t *testing.T) {
	now := t0.Add(time.Hour)
	revs := []PriceRevision{
		revFor(1, ProviderClaude, "claude-*", t0, nil),
		revFor(2, ProviderClaude, "claude-opus-*", t0, nil),
		revFor(3, ProviderClaude, "claude-opus-5-5", t0, nil),
		revFor(4, ProviderCodex, "claude-opus-5-5", t0, nil),
	}
	id := func(r *PriceRevision) int64 {
		if r == nil {
			return 0
		}
		return r.ID
	}
	if got := id(MatchPrice(revs, ProviderClaude, "Claude-Opus-5-5", now)); got != 3 {
		t.Errorf("exato: %d", got)
	}
	if got := id(MatchPrice(revs, ProviderClaude, "claude-opus-4", now)); got != 2 {
		t.Errorf("prefixo mais longo: %d", got)
	}
	if got := id(MatchPrice(revs, ProviderClaude, "claude-haiku", now)); got != 1 {
		t.Errorf("prefixo curto: %d", got)
	}
	if got := id(MatchPrice(revs, ProviderOpenCode, "claude-opus-5-5", now)); got != 0 {
		t.Errorf("outro provider: %d", got)
	}
	by := int64(9)
	dis := now
	revs[2].ReplacedBy = &by
	if got := id(MatchPrice(revs, ProviderClaude, "claude-opus-5-5", now)); got != 2 {
		t.Errorf("substituída casou: %d", got)
	}
	revs[1].DisabledAt = &dis
	if got := id(MatchPrice(revs, ProviderClaude, "claude-opus-5-5", now)); got != 1 {
		t.Errorf("desativada casou: %d", got)
	}
}

func TestMatchPriceValidity(t *testing.T) {
	to := t0.AddDate(0, 0, 10)
	revs := []PriceRevision{revFor(1, ProviderClaude, "m", t0, &to)}
	if MatchPrice(revs, ProviderClaude, "m", t0) == nil {
		t.Error("ts == ValidFrom não casou")
	}
	if MatchPrice(revs, ProviderClaude, "m", to) != nil {
		t.Error("ts == ValidTo casou")
	}
	if MatchPrice(revs, ProviderClaude, "m", t0.Add(-time.Millisecond)) != nil {
		t.Error("antes de ValidFrom casou")
	}
}

func TestValidateRevision(t *testing.T) {
	ok := func() PriceRevision { return revFor(0, ProviderClaude, "m", t0, nil) }
	end := t0.AddDate(0, 1, 0)
	before := t0.AddDate(0, 0, -1)
	bad := map[string]func(*PriceRevision){
		"moeda minúscula": func(r *PriceRevision) { r.Currency = "usd" },
		"moeda curta":     func(r *PriceRevision) { r.Currency = "US" },
		"unidade 100":     func(r *PriceRevision) { r.UnitTokens = 100 },
		"sem preços":      func(r *PriceRevision) { r.Input = nil },
		"expoente":        func(r *PriceRevision) { r.Input = str("1e3") },
		"negativo":        func(r *PriceRevision) { r.Output = str("-1") },
		"7 casas":         func(r *PriceRevision) { r.CacheRead = str("1.1234567") },
		"acima do teto":   func(r *PriceRevision) { r.Input = str("1000000.01") },
		"vigência":        func(r *PriceRevision) { r.ValidTo = &before },
	}
	for name, mut := range bad {
		r := ok()
		mut(&r)
		if err := ValidateRevision(r, nil); !errors.Is(err, ErrInvalidPrice) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// Sobreposição com revisão atual do mesmo provider e modelo.
	other := revFor(5, ProviderClaude, "M", t0.AddDate(0, 0, 5), nil)
	if err := ValidateRevision(ok(), []PriceRevision{other}); !errors.Is(err, ErrInvalidPrice) {
		t.Errorf("sobreposição: %v", err)
	}
	// Adjacentes [a,b) e [b,∞) valem.
	r := ok()
	r.ValidTo = &end
	next := revFor(6, ProviderClaude, "m", end, nil)
	if err := ValidateRevision(r, []PriceRevision{next}); err != nil {
		t.Errorf("adjacente: %v", err)
	}
	// Substituída não conta.
	by := int64(7)
	other.ReplacedBy = &by
	if err := ValidateRevision(ok(), []PriceRevision{other}); err != nil {
		t.Errorf("substituída: %v", err)
	}
	// A própria revisão (mesmo ID) não conflita consigo.
	self := revFor(8, ProviderClaude, "m", t0, nil)
	if err := ValidateRevision(self, []PriceRevision{self}); err != nil {
		t.Errorf("consigo: %v", err)
	}
}

func TestParseDecimal(t *testing.T) {
	for in, want := range map[string]struct {
		m int64
		c string
	}{"3": {3000000, "3"}, "3.50": {3500000, "3.5"}, "0.000001": {1, "0.000001"}, "0": {0, "0"}, "0.0": {0, "0"}, "1000000": {1e12, "1000000"}} {
		m, c, err := ParseDecimal(in)
		if err != nil || m != want.m || c != want.c {
			t.Errorf("%q → %d %q %v", in, m, c, err)
		}
	}
	for _, in := range []string{" 3", "", ".5", "5.", "+1", "1,5", "1e3", "-1", "1.1234567", "1000000.000001"} {
		if _, _, err := ParseDecimal(in); err == nil {
			t.Errorf("%q aceito", in)
		}
	}
}

func TestNanosJSON(t *testing.T) {
	b, _ := json.Marshal(Nanos(5))
	if string(b) != `"0.000000005"` {
		t.Errorf("%s", b)
	}
	if Nanos(12_345_000_000_000).String() != "12345.000000000" {
		t.Errorf("%s", Nanos(12_345_000_000_000))
	}
	var n Nanos
	if err := json.Unmarshal([]byte(`"0.108000000"`), &n); err != nil || n != 108_000_000 {
		t.Errorf("unmarshal %d %v", n, err)
	}
}

// Categoria com tokens sem preço e as demais com 0 tokens: não é custo 0
// parcial ("R$ 0,00*"), é sem preço.
func TestComputeEstimateNoPricedTokensIsNoPrice(t *testing.T) {
	r := rev(1_000_000, nil, str("15"), nil, nil)
	e := ComputeEstimate(r, ProviderCodex, "gpt", true, toks(i64(10), i64(0), i64(0), nil))
	if e.Status != EstimateNoPrice || e.Total != nil || !slices.Contains(e.Reasons, "entrada sem preço") {
		t.Errorf("%s %v total %v", e.Status, e.Reasons, e.Total)
	}
	// Tudo zero continua sendo custo zero completo.
	z := ComputeEstimate(r, ProviderCodex, "gpt", true, toks(i64(0), i64(0), i64(0), nil))
	if z.Status != EstimateComplete || z.Total == nil || *z.Total != 0 {
		t.Errorf("zero: %s %v", z.Status, z.Total)
	}
}
