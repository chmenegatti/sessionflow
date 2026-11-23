package claude

import (
	"encoding/json"
	"time"

	"sessionflow/internal/domain"
	"sessionflow/internal/provider"
)

// rawUsage é message.usage. Ponteiros: campo ausente fica nil (indisponível),
// nunca zero. input_tokens não inclui cache (docs/ANALYTICS.md).
type rawUsage struct {
	Input         *int64 `json:"input_tokens"`
	Output        *int64 `json:"output_tokens"`
	CacheRead     *int64 `json:"cache_read_input_tokens"`
	CacheCreation *int64 `json:"cache_creation_input_tokens"`
}

// decodeUsage lê message.usage. ok = false quando ausente, null ou ilegível
// (tipo inesperado): o consumo dessa linha fica indisponível.
func decodeUsage(raw json.RawMessage) (rawUsage, bool) {
	var u rawUsage
	if len(raw) == 0 || string(raw) == "null" {
		return u, false
	}
	if err := json.Unmarshal(raw, &u); err != nil {
		return rawUsage{}, false
	}
	return u, true
}

func (u rawUsage) same(o rawUsage) bool {
	eq := func(a, b *int64) bool { return (a == nil) == (b == nil) && (a == nil || *a == *b) }
	return eq(u.Input, o.Input) && eq(u.Output, o.Output) && eq(u.CacheRead, o.CacheRead) && eq(u.CacheCreation, o.CacheCreation)
}

// Campos que o Claude Code informa; reasoning não existe no formato.
var expected = []string{domain.UsageInput, domain.UsageOutput, domain.UsageCacheRead, domain.UsageCacheCreation}

type usageEntry struct {
	seq      int
	ts       *time.Time
	model    string
	usage    rawUsage
	conflict bool
}

// usageTracker junta as linhas de cada resposta. Uma resposta da API vem em
// várias linhas (uma por bloco), todas com o mesmo usage, e nem sempre
// contíguas; a identidade é message.id (depois requestId, depois a linha).
type usageTracker struct {
	order []string
	byID  map[string]*usageEntry
}

func (t *usageTracker) add(id string, seq int, ts *time.Time, model string, u rawUsage) {
	if t.byID == nil {
		t.byID = map[string]*usageEntry{}
	}
	if e, ok := t.byID[id]; ok {
		if !e.usage.same(u) {
			e.conflict = true
		}
		e.usage = u // vale a última linha: estado final da resposta
		return
	}
	t.order = append(t.order, id)
	t.byID[id] = &usageEntry{seq: seq, ts: ts, model: model, usage: u}
}

// emit entrega um registro por resposta, na ordem da primeira linha.
func (t *usageTracker) emit(key domain.SessionKey, sink provider.UsageSink, stats *provider.ParseStats) error {
	for _, id := range t.order {
		e := t.byID[id]
		if e.conflict {
			stats.UsageConflicts++
		}
		u := domain.Usage{
			ResponseID: id, Seq: e.seq, Timestamp: e.ts, Model: e.model,
			Input: domain.Count(e.usage.Input), Output: domain.Count(e.usage.Output),
			CacheRead: domain.Count(e.usage.CacheRead), CacheCreation: domain.Count(e.usage.CacheCreation),
		}
		if u.Input != nil && u.Output != nil && u.CacheRead != nil && u.CacheCreation != nil {
			total := *u.Input + *u.Output + *u.CacheRead + *u.CacheCreation
			u.Total, u.Derived = &total, []string{domain.UsageTotal}
		}
		u.Status = domain.StatusFor(u, expected)
		if err := sink.Usage(key, u); err != nil {
			return err
		}
	}
	return nil
}
