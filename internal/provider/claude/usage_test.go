package claude

import (
	"os"
	"path/filepath"
	"testing"

	"sessionflow/internal/domain"
	"sessionflow/internal/provider/providertest"
)

const usageMain = "33333333-3333-4333-8333-333333333333"

func usageFixture(parts ...string) string {
	return providertest.Fixture(append([]string{"usage", "claude", "-home-user-projects-demo-app"}, parts...)...)
}

func usages(t *testing.T, path string) []domain.Usage {
	t.Helper()
	sink, _ := parse(t, New(Options{}), path)
	return sink.Usages[sink.Sessions[0].Key]
}

func val(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}

func TestUsageDedupByMessageID(t *testing.T) {
	us := usages(t, usageFixture(usageMain+".jsonl"))
	if len(us) != 3 {
		t.Fatalf("%d registros; quer 3 (A, B, C)", len(us))
	}
	a := us[0]
	if a.ResponseID != "msg_FIX_A" || a.Model != "claude-opus-5-5" || a.Timestamp == nil {
		t.Errorf("identidade: %+v", a)
	}
	got := []any{val(a.Input), val(a.Output), val(a.CacheRead), val(a.CacheCreation), val(a.Reasoning), val(a.Total)}
	want := []any{int64(2), int64(403), int64(48829), int64(3492), nil, int64(52726)}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("campo %d = %v; quer %v", i, got[i], want[i])
		}
	}
	if a.Status != domain.UsageStatusComplete || len(a.Derived) != 1 || a.Derived[0] != domain.UsageTotal {
		t.Errorf("status %s derived %v", a.Status, a.Derived)
	}
	if us[1].ResponseID != "msg_FIX_B" || val(us[1].Total) != int64(76315) {
		t.Errorf("B: %+v", us[1])
	}
}

func TestUsageNonContiguousLines(t *testing.T) {
	n := 0
	for _, u := range usages(t, usageFixture(usageMain+".jsonl")) {
		if u.ResponseID == "msg_FIX_A" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("msg_FIX_A em %d registros", n)
	}
}

func TestUsageIgnoresToolUseResultAndSynthetic(t *testing.T) {
	for _, u := range usages(t, usageFixture(usageMain+".jsonl")) {
		if u.ResponseID == "msg_FIX_SYN" || u.Model == "<synthetic>" {
			t.Errorf("sintética contabilizada: %+v", u)
		}
		if val(u.Total) == int64(99999) || val(u.Input) == int64(50000) {
			t.Errorf("resumo do subagente contabilizado: %+v", u)
		}
	}
}

func TestUsageMissingField(t *testing.T) {
	us := usages(t, usageFixture(usageMain+".jsonl"))
	c := us[2]
	if c.ResponseID != "msg_FIX_C" || c.CacheCreation != nil || c.Total != nil || c.Status != domain.UsageStatusPartial {
		t.Errorf("C: %+v", c)
	}
	if val(c.Input) != int64(3) || val(c.CacheRead) != int64(1000) {
		t.Errorf("C valores: %v %v", val(c.Input), val(c.CacheRead))
	}
}

func TestUsageSubagentKeepsCopy(t *testing.T) {
	// O adaptador entrega a cópia de msg_FIX_B no subagente; quem resolve a
	// repetição entre sessões é a consulta (usage_store).
	us := usages(t, usageFixture(usageMain, "subagents", "agent-a1.jsonl"))
	if len(us) != 2 || us[0].ResponseID != "msg_FIX_B" || us[1].ResponseID != "msg_FIX_D" {
		t.Errorf("%+v", us)
	}
}

func TestUsageConflictCounted(t *testing.T) {
	dir := t.TempDir()
	lines := `{"type":"assistant","uuid":"u1","timestamp":"2026-11-20T12:00:00Z","sessionId":"s","message":{"id":"m1","model":"x","role":"assistant","content":[{"type":"text","text":"a"}],"usage":{"input_tokens":1,"output_tokens":5,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}
{"type":"assistant","uuid":"u2","timestamp":"2026-11-20T12:00:01Z","sessionId":"s","message":{"id":"m1","model":"x","role":"assistant","content":[{"type":"text","text":"b"}],"usage":{"input_tokens":1,"output_tokens":9,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}
`
	path := filepath.Join(dir, "66666666-6666-4666-8666-666666666666.jsonl")
	os.WriteFile(path, []byte(lines), 0o644)
	sink, stats := parse(t, New(Options{}), path)
	us := sink.Usages[sink.Sessions[0].Key]
	if len(us) != 1 || val(us[0].Output) != int64(9) || stats.UsageConflicts != 1 {
		t.Errorf("registros %d, saída %v, conflitos %d (quer 1, 9 — a última linha —, 1)", len(us), val(us[0].Output), stats.UsageConflicts)
	}
}
