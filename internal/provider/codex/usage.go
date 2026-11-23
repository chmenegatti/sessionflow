package codex

import (
	"encoding/json"
	"fmt"
	"time"

	"sessionflow/internal/domain"
	"sessionflow/internal/provider"
)

// tokenUsage é o formato de last_token_usage e total_token_usage. O Codex
// inclui o cache lido em input_tokens e o reasoning em output_tokens; total =
// input + output (docs/ANALYTICS.md).
type tokenUsage struct {
	Input     *int64 `json:"input_tokens"`
	Cached    *int64 `json:"cached_input_tokens"`
	Output    *int64 `json:"output_tokens"`
	Reasoning *int64 `json:"reasoning_output_tokens"`
	Total     *int64 `json:"total_tokens"`
}

type tokenInfo struct {
	Last  *tokenUsage `json:"last_token_usage"`
	Total *tokenUsage `json:"total_token_usage"`
}

// Campos que o Codex informa; cache_creation não existe no formato.
var expected = []string{domain.UsageInput, domain.UsageOutput, domain.UsageCacheRead, domain.UsageReasoning, domain.UsageTotal}

// usageTracker contabiliza os pedidos de event_msg/token_count. Só
// last_token_usage (o pedido) é somado; o acumulado da sessão serve de
// identidade (os eventos não têm ID) e de reconciliação: um evento cujo
// acumulado não avança é repetição.
type usageTracker struct {
	records  []domain.Usage
	ids      []string // sufixo da identidade de cada registro: acumulado (e trecho, após retomada)
	segment  int
	cum      int64 // último acumulado aceito
	segSum   int64 // soma dos pedidos desde o início (ou desde a última retomada)
	mismatch bool
}

func (t *usageTracker) add(raw json.RawMessage, seq int, ts *time.Time, model string) {
	var info tokenInfo
	if len(raw) == 0 || json.Unmarshal(raw, &info) != nil || info.Last == nil || info.Total == nil || info.Total.Total == nil {
		return // info nulo ou incompleto: não é pedido contabilizável
	}
	cum := *info.Total.Total
	switch {
	case cum == t.cum:
		return // repetição
	case cum < t.cum:
		// Sessão retomada que recomeça a contagem: fecha o trecho anterior.
		t.check()
		t.segSum = 0
		t.segment++
	}
	t.cum = cum
	l := info.Last
	u := domain.Usage{
		Seq: seq, Timestamp: ts, Model: model,
		Output: domain.Count(l.Output), CacheRead: domain.Count(l.Cached), Reasoning: domain.Count(l.Reasoning), Total: domain.Count(l.Total),
	}
	if in, cached := domain.Count(l.Input), domain.Count(l.Cached); in != nil && cached != nil && *in >= *cached {
		d := *in - *cached
		u.Input, u.Derived = &d, []string{domain.UsageInput}
	}
	u.Status = domain.StatusFor(u, expected)
	if u.Total != nil {
		t.segSum += *u.Total
	}
	id := fmt.Sprint(cum)
	if t.segment > 0 {
		id = fmt.Sprintf("%d@%d", cum, t.segment)
	}
	t.records, t.ids = append(t.records, u), append(t.ids, id)
}

func (t *usageTracker) check() {
	if t.segSum != t.cum {
		t.mismatch = true
	}
}

func (t *usageTracker) emit(key domain.SessionKey, sink provider.UsageSink, stats *provider.ParseStats) error {
	if len(t.records) > 0 {
		t.check()
	}
	stats.UsageMismatch = t.mismatch
	for i, u := range t.records {
		// Identidade: sessão + acumulado (o ID da sessão só é conhecido no fim).
		u.ResponseID = key.NativeID + "#" + t.ids[i]
		if err := sink.Usage(key, u); err != nil {
			return err
		}
	}
	return nil
}
