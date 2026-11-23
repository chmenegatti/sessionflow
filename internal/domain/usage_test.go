package domain

import "testing"

func TestStatusFor(t *testing.T) {
	n := int64(5)
	u := Usage{Input: &n}
	if got := StatusFor(u, []string{"input", "output"}); got != UsageStatusPartial {
		t.Errorf("sem output: %s", got)
	}
	u.Output = &n
	if got := StatusFor(u, []string{"input", "output"}); got != UsageStatusComplete {
		t.Errorf("completo: %s", got)
	}
	// Campo fora de expected (indisponível por natureza) não torna parcial.
	if got := StatusFor(u, []string{"input", "output"}); u.Reasoning != nil || got != UsageStatusComplete {
		t.Errorf("reasoning fora: %s", got)
	}
}
