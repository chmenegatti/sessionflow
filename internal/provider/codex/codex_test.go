package codex

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sessionflow/internal/domain"
	"sessionflow/internal/provider"
	"sessionflow/internal/provider/providertest"
)

const fixtureID = "22222222-2222-7222-8222-222222222222"

func place(t *testing.T, root, rel string, data []byte) string {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func fixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(providertest.Fixture("codex", "rollout.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func parse(t *testing.T, a *Adapter, path string) (*providertest.Sink, provider.ParseStats) {
	t.Helper()
	src, err := provider.StatSource(path, domain.SourceJSONL)
	if err != nil {
		t.Fatal(err)
	}
	sink := providertest.NewSink()
	stats, err := a.Parse(context.Background(), src, sink)
	if err != nil {
		t.Fatal(err)
	}
	if len(sink.Sessions) != 1 {
		t.Fatalf("sessões = %d", len(sink.Sessions))
	}
	return sink, stats
}

func TestParseFixture(t *testing.T) {
	root := t.TempDir()
	path := place(t, root, "sessions/2026/01/10/rollout-2026-01-10T12-00-00-"+fixtureID+".jsonl", fixture(t))
	a := New(Options{RootDir: root})
	sink, stats := parse(t, a, path)

	s := sink.Sessions[0]
	if s.Key.String() != "codex:"+fixtureID || s.ProjectPath != "/home/user/projects/demo-app" ||
		s.Model != "gpt-5.4" || s.Title != "Liste os arquivos Go do projeto" || s.Archived || s.Automated {
		t.Errorf("sessão: %+v", s)
	}
	if s.MessageCount != 2 {
		t.Errorf("MessageCount = %d", s.MessageCount)
	}
	if stats.BadLines != 0 || stats.Partial {
		t.Errorf("stats = %+v", stats)
	}

	var kinds []string
	for _, m := range sink.Messages[s.Key] {
		k := string(m.Role)
		if len(m.Blocks) > 0 {
			k += "/" + string(m.Blocks[0].Kind)
		}
		kinds = append(kinds, k)
	}
	// developer, user injetado, response_item user (duplicata, só
	// metadado), user_message, function_call, output, resposta, item
	// desconhecido. reasoning criptografado e agent_message não aparecem.
	want := "system/metadata,system/metadata,system/metadata,system/metadata,user/text,system/metadata," +
		"assistant/tool_call,tool/tool_result,assistant/text,system/metadata,system/metadata,system/metadata,system/unknown"
	if got := strings.Join(kinds, ","); got != want {
		t.Fatalf("mensagens:\n got %s\nwant %s", got, want)
	}
	msgs := sink.Messages[s.Key]
	call, res := msgs[6].Blocks[0], msgs[7].Blocks[0]
	if call.ToolName != "exec_command" || call.ToolCallID != res.ToolCallID || !strings.Contains(string(call.ToolInput), `"cmd"`) {
		t.Errorf("tool call: %+v", call)
	}
	if res.Text != "./main.go\n./app.go" {
		t.Errorf("tool result: %q", res.Text)
	}
}

func TestSourcesArchivedAndCompressed(t *testing.T) {
	root := t.TempDir()
	data := fixture(t)
	place(t, root, "sessions/2026/01/10/rollout-2026-01-10T12-00-00-"+fixtureID+".jsonl", data)
	arch := place(t, root, "archived_sessions/rollout-2026-01-09T12-00-00-33333333-3333-7333-8333-333333333333.jsonl",
		[]byte(`{"timestamp":"2026-01-09T12:00:00Z","type":"session_meta","payload":{"id":"33333333-3333-7333-8333-333333333333","cwd":"/tmp/x"}}`+"\n"))
	place(t, root, "sessions/2026/01/01/rollout-2026-01-01T00-00-00-44444444-4444-7444-8444-444444444444.jsonl.zst", []byte("zstd"))
	place(t, root, "history.jsonl", []byte(`{"session_id":"x","ts":1,"text":"y"}`+"\n"))

	a := New(Options{RootDir: root})
	srcs, err := a.Sources(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(srcs) != 2 {
		t.Fatalf("fontes = %+v", srcs)
	}
	sink, _ := parse(t, a, arch)
	if s := sink.Sessions[0]; !s.Archived || !s.Automated || s.ProjectPath != "/tmp/x" {
		t.Errorf("arquivada: %+v", s)
	}
	d := a.Detect(context.Background())
	if d.Status != provider.StatusOK || !strings.Contains(d.Detail, "1 rollout") {
		t.Errorf("detect: %+v", d)
	}
}

func TestIDFromName(t *testing.T) {
	cases := map[string]string{
		"rollout-2026-01-10T12-00-00-" + fixtureID + ".jsonl":                                      fixtureID,
		"rollout-2026-01-10T12-00-00-" + fixtureID + "_55555555-5555-7555-8555-555555555555.jsonl": fixtureID,
		"rollout-curto.jsonl": "",
	}
	for name, want := range cases {
		if got := idFromName(name); got != want {
			t.Errorf("%s: got %q want %q", name, got, want)
		}
	}
}

// Sem ID no nome e sem session_meta: a fonte é rejeitada com erro, sem panic.
func TestParseWithoutID(t *testing.T) {
	path := place(t, t.TempDir(), "rollout-sem-id.jsonl",
		[]byte(`{"type":"event_msg","payload":{"type":"user_message","message":"oi"}}`+"\nlixo\n"))
	src, _ := provider.StatSource(path, domain.SourceJSONL)
	stats, err := New(Options{}).Parse(context.Background(), src, providertest.NewSink())
	if err == nil || stats.SkippedItems != 1 || stats.BadLines != 1 {
		t.Fatalf("err=%v stats=%+v", err, stats)
	}
}

func TestParseTruncatedAndBadPayload(t *testing.T) {
	root := t.TempDir()
	body := `{"timestamp":"2026-01-10T12:00:00Z","type":"session_meta","payload":{"id":"` + fixtureID + `","cwd":"/p"}}` + "\n" +
		`{"timestamp":"x","type":"event_msg","payload":"não-objeto"}` + "\n" +
		`{"timestamp":"2026-01-10T12:00:01Z","type":"event_msg","payload":{"type":"user_message","message":"\n\n  primeira linha\nsegunda"}}` + "\n" +
		`{"timestamp":"2026-01-10T12:00:02Z","type":"response_item","payload":{"type":"function_call_output","call_id":"c","output":{"content":"ok","success":true}}}` + "\n" +
		`{"timestamp":"2026-01-10T12:00:03Z","type":"response_item","payload":{"type":"custom_tool_call","name":"apply_patch","call_id":"c2","input":"*** Begin Patch"}}` + "\n" +
		`{"timestamp":"2026-01-10T12:00:04Z","type":"resp`
	path := place(t, root, "sessions/rollout-2026-01-10T12-00-00-"+fixtureID+".jsonl", []byte(body))
	sink, stats := parse(t, New(Options{RootDir: root}), path)
	s := sink.Sessions[0]
	if !stats.Partial || stats.BadLines != 1 {
		t.Errorf("stats = %+v", stats)
	}
	if s.Title != "primeira linha" {
		t.Errorf("título = %q", s.Title)
	}
	msgs := sink.Messages[s.Key]
	if msgs[1].Blocks[0].Text != "ok" {
		t.Errorf("output objeto: %+v", msgs[1].Blocks[0])
	}
	if string(msgs[2].Blocks[0].ToolInput) != `"*** Begin Patch"` {
		t.Errorf("input texto deveria virar string JSON: %s", msgs[2].Blocks[0].ToolInput)
	}
}
