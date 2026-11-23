package opencode

import (
	"encoding/json"
	"time"

	"sessionflow/internal/domain"
)

// rawTokens é message.data.tokens. input não inclui cache; total informado =
// input + output + cache (145/145 nos dados observados). Se reasoning está
// dentro de output não dá para verificar localmente (todos 0): reasoning
// nunca é somado, e o total só é derivado com reasoning = 0.
type rawTokens struct {
	Total     *int64 `json:"total"`
	Input     *int64 `json:"input"`
	Output    *int64 `json:"output"`
	Reasoning *int64 `json:"reasoning"`
	Cache     *struct {
		Read  *int64 `json:"read"`
		Write *int64 `json:"write"`
	} `json:"cache"`
}

// decodeTokens lê message.data.tokens. ok = false quando ausente, null ou
// ilegível (tipo inesperado): o consumo dessa mensagem fica indisponível.
func decodeTokens(raw json.RawMessage) (*rawTokens, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, false
	}
	var t rawTokens
	if err := json.Unmarshal(raw, &t); err != nil {
		return nil, false
	}
	return &t, true
}

// Campos que o OpenCode informa. Reasoning é categoria à parte.
var expected = []string{domain.UsageInput, domain.UsageOutput, domain.UsageCacheRead, domain.UsageCacheCreation, domain.UsageTotal}

// usageOf converte os tokens de uma resposta do assistente.
func usageOf(id string, seq int, ts *time.Time, model string, t *rawTokens) domain.Usage {
	u := domain.Usage{ResponseID: id, Seq: seq, Timestamp: ts, Model: model,
		Input: domain.Count(t.Input), Output: domain.Count(t.Output), Reasoning: domain.Count(t.Reasoning), Total: domain.Count(t.Total)}
	if t.Cache != nil {
		u.CacheRead, u.CacheCreation = domain.Count(t.Cache.Read), domain.Count(t.Cache.Write)
	}
	if u.Total == nil && u.Reasoning != nil && *u.Reasoning == 0 &&
		u.Input != nil && u.Output != nil && u.CacheRead != nil && u.CacheCreation != nil {
		total := *u.Input + *u.Output + *u.CacheRead + *u.CacheCreation
		u.Total, u.Derived = &total, []string{domain.UsageTotal}
	}
	u.Status = domain.StatusFor(u, expected)
	return u
}
