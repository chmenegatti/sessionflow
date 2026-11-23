package claude

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"sessionflow/internal/domain"
	"sessionflow/internal/provider"
	"sessionflow/internal/provider/providertest"
)

const fixtureID = "11111111-1111-4111-8111-111111111111"

// layout monta ~/.claude/projects/<dir>/<id>.jsonl a partir do fixture.
func layout(t *testing.T) (root string) {
	t.Helper()
	root = t.TempDir()
	dir := filepath.Join(root, "projects", "-home-user-projects-demo-app")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(providertest.Fixture("claude", "session.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, fixtureID+".jsonl"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return root
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
		t.Fatalf("esperava 1 sessão, veio %d", len(sink.Sessions))
	}
	return sink, stats
}

func TestParseFixture(t *testing.T) {
	root := layout(t)
	a := New(Options{RootDir: root})
	path := filepath.Join(root, "projects", "-home-user-projects-demo-app", fixtureID+".jsonl")
	sink, stats := parse(t, a, path)

	s := sink.Sessions[0]
	if s.Key.String() != "claude:"+fixtureID {
		t.Errorf("key = %s", s.Key)
	}
	if s.ProjectPath != "/home/user/projects/demo-app" || s.Title != "Listar arquivos Go" ||
		s.FirstPrompt != "Liste os arquivos Go do projeto" || s.Model != "claude-opus-5-5" {
		t.Errorf("metadados: %+v", s)
	}
	if s.Automated || s.Parent != nil || s.SourcePath != path || s.SourceKind != domain.SourceJSONL {
		t.Errorf("flags: %+v", s)
	}
	if got := s.CreatedAt.Format("15:04:05"); got != "12:00:00" {
		t.Errorf("CreatedAt = %s", got)
	}
	if got := s.UpdatedAt.Format("15:04:05.000"); got != "12:00:05.500" {
		t.Errorf("UpdatedAt = %s", got)
	}
	// user + assistant(texto); meta, tool_use e tool_result não contam.
	if s.MessageCount != 2 {
		t.Errorf("MessageCount = %d", s.MessageCount)
	}
	if !stats.Partial || stats.BadLines != 0 {
		t.Errorf("stats = %+v", stats)
	}

	msgs := sink.Messages[s.Key]
	var roles []string
	for i, m := range msgs {
		if m.Seq != i {
			t.Errorf("Seq fora de ordem: %d em %d", m.Seq, i)
		}
		roles = append(roles, string(m.Role))
	}
	// permission-mode, meta, prompt, tool_use, tool_result, resposta,
	// turn_duration, tipo desconhecido, last-prompt. Nada é descartado.
	want := "system,system,user,assistant,tool,assistant,system,system,system"
	if strings.Join(roles, ",") != want {
		t.Fatalf("roles = %s, want %s", strings.Join(roles, ","), want)
	}

	// Linhas thinking(vazio) + tool_use com o mesmo message.id viram uma mensagem.
	call := msgs[3]
	if len(call.Blocks) != 1 || call.Blocks[0].Kind != domain.BlockToolCall ||
		call.Blocks[0].ToolName != "Bash" || !strings.Contains(string(call.Blocks[0].ToolInput), "find") {
		t.Errorf("tool call: %+v", call.Blocks)
	}
	res := msgs[4].Blocks[0]
	if res.Kind != domain.BlockToolResult || res.ToolCallID != call.Blocks[0].ToolCallID || res.Text != "./main.go\n./app.go" {
		t.Errorf("tool result: %+v", res)
	}
	if msgs[1].Blocks[0].Kind != domain.BlockMetadata {
		t.Errorf("isMeta deveria virar metadata: %+v", msgs[0].Blocks)
	}
	unk := msgs[7].Blocks[0]
	if unk.Kind != domain.BlockUnknown || unk.Text != "future-unknown-type" || len(unk.Raw) == 0 {
		t.Errorf("tipo desconhecido: %+v", unk)
	}
}

func write(t *testing.T, path string, lines ...string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestParseToleratesBadInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "projects", "p", "abc.jsonl")
	write(t, path,
		`{"type":"user","message":{"role":"user","content":"oi"},"cwd":"relativo","timestamp":"2026-01-01T00:00:00Z"}`,
		`não é json`,
		`{"type":"assistant"}`,
		`{"type":"assistant","message":{"id":"m1","content":"texto simples"}}`,
		`{"type":"user","message":{"content":[{"type":"image","source":{"data":"AAAA"}},{"type":"novo"}]}}`,
		`{"type":"custom-title","customTitle":"Meu título"}`,
		`{"type":"ai-title","aiTitle":"Título da IA"}`,
	)
	sink, stats := parse(t, New(Options{}), path)
	s := sink.Sessions[0]
	if stats.BadLines != 1 || stats.SkippedItems != 1 || stats.Partial {
		t.Errorf("stats = %+v", stats)
	}
	if s.Title != "Meu título" {
		t.Errorf("custom-title deve vencer ai-title: %q", s.Title)
	}
	if s.ProjectPath != "" {
		t.Errorf("cwd relativo deveria ser descartado: %q", s.ProjectPath)
	}
	msgs := sink.Messages[s.Key]
	img := msgs[2].Blocks
	if len(img) != 2 || img[0].Kind != domain.BlockImageRef || img[0].Raw != nil || img[1].Kind != domain.BlockUnknown {
		t.Errorf("blocos de imagem/desconhecido: %+v", img)
	}
}

func TestParseEmptyAndAutomated(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "vazio.jsonl")
	write(t, empty, `{"type":"permission-mode","permissionMode":"default"}`)
	sink, _ := parse(t, New(Options{}), empty)
	if s := sink.Sessions[0]; !s.Automated || s.MessageCount != 0 || s.UpdatedAt.IsZero() {
		t.Errorf("sessão vazia: %+v", s)
	}

	sdk := filepath.Join(dir, "sdk.jsonl")
	write(t, sdk, `{"type":"user","entrypoint":"sdk-ts","message":{"content":"tarefa"}}`)
	sink, _ = parse(t, New(Options{}), sdk)
	if !sink.Sessions[0].Automated {
		t.Error("entrypoint sdk deveria marcar Automated")
	}
}

func TestSourcesAndSubagents(t *testing.T) {
	root := layout(t)
	proj := filepath.Join(root, "projects", "-home-user-projects-demo-app")
	write(t, filepath.Join(proj, fixtureID, "subagents", "agent-a1.jsonl"),
		`{"type":"user","isSidechain":true,"agentId":"a1","sessionId":"`+fixtureID+`","message":{"content":"sub"}}`)
	write(t, filepath.Join(proj, fixtureID, "subagents", "agent-a1.json"), `{}`)
	write(t, filepath.Join(proj, "x.orphaned-1-abc.jsonl"), `{}`)
	write(t, filepath.Join(proj, "y.jsonl.superseded-1"), `{}`)
	os.Symlink(filepath.Join(proj, fixtureID+".jsonl"), filepath.Join(proj, "link.jsonl"))

	a := New(Options{RootDir: root})
	srcs, err := a.Sources(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(srcs) != 2 {
		t.Fatalf("fontes = %+v", srcs)
	}
	sub := srcs[1].Path
	if !strings.HasSuffix(sub, "agent-a1.jsonl") {
		srcs[0], srcs[1] = srcs[1], srcs[0]
		sub = srcs[1].Path
	}
	sink, _ := parse(t, a, sub)
	s := sink.Sessions[0]
	if s.Key.NativeID != fixtureID+"/agent-a1" || s.Parent == nil || s.Parent.NativeID != fixtureID {
		t.Errorf("subagente: key=%s parent=%v", s.Key, s.Parent)
	}
}

func TestDetect(t *testing.T) {
	a := New(Options{RootDir: t.TempDir()})
	if d := a.Detect(context.Background()); d.Status != provider.StatusNotFound {
		t.Errorf("sem projects/: %+v", d)
	}
	root := layout(t)
	if d := New(Options{RootDir: root}).Detect(context.Background()); d.Status != provider.StatusOK {
		t.Errorf("com projects/: %+v", d)
	}
}

func TestParseCancelled(t *testing.T) {
	root := layout(t)
	path := filepath.Join(root, "projects", "-home-user-projects-demo-app", fixtureID+".jsonl")
	src, _ := provider.StatSource(path, domain.SourceJSONL)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New(Options{}).Parse(ctx, src, providertest.NewSink()); err == nil {
		t.Fatal("esperava erro de contexto")
	}
}

// Sessão de SDK: todo prompt é isMeta. Fica automática, mas com título.
func TestParseSDKSessionGetsTitle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sdk.jsonl")
	write(t, path,
		`{"type":"user","isMeta":true,"entrypoint":"sdk-ts","message":{"content":"\n  Observe a sessão X\nmais"}}`,
		`{"type":"assistant","message":{"id":"m","content":[{"type":"text","text":"ok"}]}}`)
	sink, _ := parse(t, New(Options{}), path)
	if s := sink.Sessions[0]; !s.Automated || s.FirstPrompt != "" || s.Title != "Observe a sessão X" {
		t.Errorf("sessão SDK: %+v", s)
	}
}

func TestSourcesReportsUnreadableDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod não bloqueia leitura no Windows (só marca somente leitura)")
	}
	if os.Getuid() == 0 {
		t.Skip("root ignora permissões")
	}
	root := layout(t)
	bad := filepath.Join(root, "projects", "-bloqueado")
	os.MkdirAll(bad, 0o755)
	os.Chmod(bad, 0)
	t.Cleanup(func() { os.Chmod(bad, 0o755) })
	srcs, err := New(Options{RootDir: root}).Sources(context.Background())
	if !errors.Is(err, provider.ErrIncomplete) || len(srcs) != 1 {
		t.Errorf("srcs=%d err=%v", len(srcs), err)
	}
}

// Viewer: imagens, documentos e registros conhecidos (attachment) chegam
// com o tipo e, no modo Raw, com o JSON original.
func TestParseAttachmentsAndRaw(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.jsonl")
	write(t, path,
		`{"type":"user","message":{"content":[{"type":"text","text":"veja"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AAAA"}},{"type":"document","title":"spec.pdf","source":{"media_type":"application/pdf","data":"BBBB"}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":[{"type":"text","text":"print"},{"type":"image","source":{"media_type":"image/jpeg","data":"CC"}}]}]}}`,
		`{"type":"attachment","attachment":{"type":"hook_success","content":"ok"}}`)

	for _, raw := range []bool{false, true} {
		src, _ := provider.StatSource(path, domain.SourceJSONL)
		src.Raw = raw
		sink := providertest.NewSink()
		if _, err := New(Options{}).Parse(context.Background(), src, sink); err != nil {
			t.Fatal(err)
		}
		msgs := sink.Messages[sink.Sessions[0].Key]
		u := msgs[0].Blocks
		if len(u) != 3 || u[1].Kind != domain.BlockImageRef || u[1].Language != "image/png" || u[1].Raw != nil ||
			u[2].Kind != domain.BlockFile || u[2].Text != "spec.pdf" || u[2].Language != "application/pdf" {
			t.Errorf("blocos do usuário: %+v", u)
		}
		r := msgs[1].Blocks
		if len(r) != 2 || r[0].Text != "print" || r[1].Kind != domain.BlockImageRef || r[1].Language != "image/jpeg" {
			t.Errorf("imagem no tool_result: %+v", r)
		}
		a := msgs[2].Blocks[0]
		if a.Kind != domain.BlockMetadata || a.Text != "attachment: hook_success" || (len(a.Raw) > 0) != raw {
			t.Errorf("raw=%v attachment: %+v", raw, a)
		}
	}
}
