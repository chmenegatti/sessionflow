package opencode

import (
	"testing"

	"sessionflow/internal/domain"
)

// Contagem negativa vira indisponível e não deriva total.
func TestOpenCodeNegativeIsUnavailable(t *testing.T) {
	neg, zero, one := int64(-4), int64(0), int64(1)
	u := usageOf("m", 0, nil, "x", &rawTokens{Input: &neg, Output: &one, Reasoning: &zero,
		Cache: &struct {
			Read  *int64 `json:"read"`
			Write *int64 `json:"write"`
		}{&zero, &zero}})
	if u.Input != nil || u.Total != nil {
		t.Errorf("entrada %v total %v; quer indisponíveis", ov(u.Input), ov(u.Total))
	}
	if u.Status != domain.UsageStatusPartial {
		t.Errorf("status %s", u.Status)
	}
}
