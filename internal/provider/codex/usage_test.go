package codex

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"sessionflow/internal/domain"
	"sessionflow/internal/provider"
	"sessionflow/internal/provider/providertest"
)

const (
	usageRollout = "rollout-2026-11-20T15-00-00-66666666-6666-7666-8666-666666666666.jsonl"
	oldRollout   = "rollout-2026-11-20T16-00-00-77777777-7777-7777-8777-777777777777.jsonl"
)

func usageParse(t *testing.T, path string) ([]domain.Usage, provider.ParseStats) {
	t.Helper()
	src, err := provider.StatSource(path, domain.SourceJSONL)
	if err != nil {
		t.Fatal(err)
	}
	sink := providertest.NewSink()
	stats, err := New(Options{}).Parse(context.Background(), src, sink)
	if err != nil {
		t.Fatal(err)
	}
	return sink.Usages[sink.Sessions[0].Key], stats
}

func v(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}

func TestCodexUsageFromLastTokenUsage(t *testing.T) {
	us, _ := usageParse(t, providertest.Fixture("usage", "codex", "2026", "11", "20", usageRollout))
	if len(us) != 3 {
		t.Fatalf("%d registros; quer 3 (repetido e info nulo ignorados)", len(us))
	}
	want := [][]any{
		{int64(1051), int64(316), int64(7808), nil, int64(70), int64(9175)},
		{int64(4453), int64(308), int64(9088), nil, int64(46), int64(13849)},
		{int64(10985), int64(440), int64(13568), nil, int64(243), int64(24993)},
	}
	for i, u := range us {
		got := []any{v(u.Input), v(u.Output), v(u.CacheRead), v(u.CacheCreation), v(u.Reasoning), v(u.Total)}
		for j := range got {
			if got[j] != want[i][j] {
				t.Errorf("pedido %d campo %d = %v; quer %v", i+1, j, got[j], want[i][j])
			}
		}
		if u.Status != domain.UsageStatusComplete || len(u.Derived) != 1 || u.Derived[0] != domain.UsageInput {
			t.Errorf("pedido %d: status %s derived %v", i+1, u.Status, u.Derived)
		}
	}
}

func TestCodexReconciles(t *testing.T) {
	us, stats := usageParse(t, providertest.Fixture("usage", "codex", "2026", "11", "20", usageRollout))
	var sum int64
	for _, u := range us {
		sum += *u.Total
	}
	if sum != 48017 || stats.UsageMismatch {
		t.Errorf("soma %d (quer 48017), mismatch %v", sum, stats.UsageMismatch)
	}
}

func TestCodexModelFromTurnContext(t *testing.T) {
	us, _ := usageParse(t, providertest.Fixture("usage", "codex", "2026", "11", "20", usageRollout))
	for _, u := range us {
		if u.Model != "gpt-5.4" || u.Timestamp == nil {
			t.Errorf("modelo %q ts %v", u.Model, u.Timestamp)
		}
	}
}

func TestCodexNoUsage(t *testing.T) {
	us, stats := usageParse(t, providertest.Fixture("usage", "codex", "2026", "11", "20", oldRollout))
	if len(us) != 0 || stats.UsageMismatch {
		t.Errorf("%d registros, mismatch %v", len(us), stats.UsageMismatch)
	}
}

func TestCodexCumulativeDecrease(t *testing.T) {
	tc := func(ts string, last, total int) string {
		return `{"timestamp":"` + ts + `","type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":` +
			itoa(last) + `,"cached_input_tokens":0,"output_tokens":0,"reasoning_output_tokens":0,"total_tokens":` + itoa(last) +
			`},"total_token_usage":{"input_tokens":` + itoa(total) + `,"cached_input_tokens":0,"output_tokens":0,"reasoning_output_tokens":0,"total_tokens":` + itoa(total) + `}}}}`
	}
	id := "88888888-8888-7888-8888-888888888888"
	data := `{"timestamp":"2026-11-20T10:00:00Z","type":"session_meta","payload":{"id":"` + id + `","cwd":"/p"}}` + "\n" +
		tc("2026-11-20T10:00:01Z", 100, 100) + "\n" +
		tc("2026-11-20T10:00:02Z", 50, 150) + "\n" +
		tc("2026-11-20T11:00:00Z", 30, 30) + "\n" + // retomada: o acumulado recomeça
		tc("2026-11-20T11:00:01Z", 30, 30) + "\n" // repetido
	path := filepath.Join(t.TempDir(), "rollout-2026-11-20T10-00-00-"+id+".jsonl")
	os.WriteFile(path, []byte(data), 0o644)
	us, stats := usageParse(t, path)
	if len(us) != 3 || stats.UsageMismatch {
		t.Errorf("%d registros (quer 3), mismatch %v", len(us), stats.UsageMismatch)
	}
}

func itoa(n int) string {
	b := []byte{}
	if n == 0 {
		return "0"
	}
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}

// A identidade entra na deduplicação entre sessões (usage_store): precisa
// incluir a sessão, senão pedidos de sessões diferentes com o mesmo
// acumulado colidiriam.
func TestCodexResponseIDIncludesSession(t *testing.T) {
	us, _ := usageParse(t, providertest.Fixture("usage", "codex", "2026", "11", "20", usageRollout))
	want := "66666666-6666-7666-8666-666666666666#9175"
	if us[0].ResponseID != want {
		t.Errorf("ResponseID %q; quer %q", us[0].ResponseID, want)
	}
}
