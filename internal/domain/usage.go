package domain

import "time"

// UsageStatus diz se um registro de consumo trouxe todos os campos que o
// provider costuma informar.
type UsageStatus string

const (
	UsageStatusComplete UsageStatus = "complete"
	UsageStatusPartial  UsageStatus = "partial"
)

// Nomes dos campos de consumo (Derived e listas de campos esperados).
const (
	UsageInput         = "input"
	UsageOutput        = "output"
	UsageCacheRead     = "cache_read"
	UsageCacheCreation = "cache_creation"
	UsageReasoning     = "reasoning"
	UsageTotal         = "total"
)

// Usage é o consumo de tokens de uma resposta (Claude, OpenCode) ou de um
// pedido (Codex), já deduplicado pelo adaptador. Campo nil = o provider não
// informou (nunca é zero). Input não inclui cache; Reasoning nunca entra em
// Total. Detalhes por provider em docs/ANALYTICS.md.
type Usage struct {
	ResponseID string     // identidade estável da resposta/pedido
	Seq        int        // posição na fonte (ordem determinística)
	Timestamp  *time.Time // nil = sem horário
	Model      string

	Input, Output, CacheRead, CacheCreation, Reasoning, Total *int64

	Derived []string    // campos calculados pelo adaptador (UsageInput, UsageTotal…)
	Status  UsageStatus // preenchido com StatusFor
}

// field devolve o campo pelo nome.
func (u Usage) field(name string) *int64 {
	switch name {
	case UsageInput:
		return u.Input
	case UsageOutput:
		return u.Output
	case UsageCacheRead:
		return u.CacheRead
	case UsageCacheCreation:
		return u.CacheCreation
	case UsageReasoning:
		return u.Reasoning
	case UsageTotal:
		return u.Total
	}
	return nil
}

// StatusFor é complete quando todos os campos que o provider informa
// (expected) vieram; campos que o provider nunca informa ficam de fora.
func StatusFor(u Usage, expected []string) UsageStatus {
	for _, f := range expected {
		if u.field(f) == nil {
			return UsageStatusPartial
		}
	}
	return UsageStatusComplete
}

// Count valida uma contagem de tokens informada pelo provider: negativa não
// é consumo e vira indisponível (nil).
func Count(p *int64) *int64 {
	if p == nil || *p < 0 {
		return nil
	}
	return p
}
