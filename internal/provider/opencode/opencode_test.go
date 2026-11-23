package opencode

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"sessionflow/internal/domain"
	"sessionflow/internal/provider"
	"sessionflow/internal/provider/providertest"
)

// buildDB cria root/opencode.db a partir do fixture SQL mais statements extras.
func buildDB(t *testing.T, root string, extra ...string) string {
	t.Helper()
	schema, err := os.ReadFile(providertest.Fixture("opencode", "schema-v1.sql"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "opencode.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, q := range append([]string{string(schema)}, extra...) {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%.60s: %v", q, err)
		}
	}
	return path
}

func adapter(t *testing.T, root string) *Adapter {
	return New(Options{RootDir: root, SnapshotDir: t.TempDir()})
}

func parseAll(t *testing.T, a *Adapter) (*providertest.Sink, provider.ParseStats, error) {
	t.Helper()
	srcs, err := a.Sources(context.Background())
	if err != nil || len(srcs) != 1 {
		t.Fatalf("fontes = %v, %v", srcs, err)
	}
	sink := providertest.NewSink()
	stats, err := a.Parse(context.Background(), srcs[0], sink)
	return sink, stats, err
}

func TestParseFixture(t *testing.T) {
	root := t.TempDir()
	buildDB(t, root)
	sink, stats, err := parseAll(t, adapter(t, root))
	if err != nil {
		t.Fatal(err)
	}
	if len(sink.Sessions) != 1 {
		t.Fatalf("sessões = %d", len(sink.Sessions))
	}
	s := sink.Sessions[0]
	if s.Key.String() != "opencode:ses_fixture000001AAAAAAAAAAAAAA" || s.ProjectPath != "/home/user/projects/demo-app" ||
		s.Title != "Listar arquivos Go" || s.FirstPrompt != "Liste os arquivos Go do projeto" ||
		s.Model != "claude-opus-5-5" || s.MessageCount != 2 || s.Archived || s.Automated || s.Parent != nil ||
		s.SourceKind != domain.SourceSQLite {
		t.Errorf("sessão: %+v", s)
	}
	if s.CreatedAt.UnixMilli() != 1768046400000 || s.UpdatedAt.UnixMilli() != 1768046405000 {
		t.Errorf("tempos: %v %v", s.CreatedAt, s.UpdatedAt)
	}
	if stats.BadLines != 0 {
		t.Errorf("stats = %+v", stats)
	}

	msgs := sink.Messages[s.Key]
	if len(msgs) != 2 || msgs[0].Role != domain.RoleUser || msgs[1].Role != domain.RoleAssistant {
		t.Fatalf("mensagens: %+v", msgs)
	}
	var kinds []string
	for _, b := range msgs[1].Blocks {
		kinds = append(kinds, string(b.Kind))
	}
	want := "metadata,reasoning,tool_call,tool_result,text,metadata,unknown"
	if got := strings.Join(kinds, ","); got != want {
		t.Fatalf("blocos:\n got %s\nwant %s", got, want)
	}
	call, res := msgs[1].Blocks[2], msgs[1].Blocks[3]
	if call.ToolName != "bash" || call.ToolCallID != res.ToolCallID || res.Text != "./main.go\n./app.go" {
		t.Errorf("tool: %+v / %+v", call, res)
	}
}

func TestParseSubsessionArchivedAndBadJSON(t *testing.T) {
	root := t.TempDir()
	buildDB(t, root,
		`INSERT INTO session (id, project_id, parent_id, slug, directory, title, version, time_created, time_updated, time_archived)
		 VALUES ('ses_child', 'p', 'ses_fixture000001AAAAAAAAAAAAAA', 's', '/home/user/projects/demo-app', '', '1.14.24', 1768046500000, 1768046600000, 1768046700000)`,
		`INSERT INTO message VALUES ('msg_bad', 'ses_child', 1768046500000, 1768046500000, '{quebrado')`,
		`INSERT INTO message VALUES ('msg_ok', 'ses_child', 1768046501000, 1768046501000, '{"role":"assistant","modelID":"m2"}')`,
		`INSERT INTO part VALUES ('prt_bad', 'msg_ok', 'ses_child', 1768046501000, 1768046501000, 'nem json')`,
		`INSERT INTO part VALUES ('prt_err', 'msg_ok', 'ses_child', 1768046501500, 1768046501500,
		  '{"type":"tool","callID":"c","tool":"bash","state":{"status":"error","input":{},"error":"falhou"}}')`,
	)
	sink, stats, err := parseAll(t, adapter(t, root))
	if err != nil {
		t.Fatal(err)
	}
	if stats.BadLines != 2 {
		t.Errorf("BadLines = %d", stats.BadLines)
	}
	var child domain.Session
	for _, s := range sink.Sessions {
		if s.Key.NativeID == "ses_child" {
			child = s
		}
	}
	if child.Parent == nil || child.Parent.NativeID != "ses_fixture000001AAAAAAAAAAAAAA" || !child.Archived ||
		!child.Automated || child.Model != "m2" {
		t.Errorf("subsessão: %+v", child)
	}
	blocks := sink.Messages[child.Key][0].Blocks
	if len(blocks) != 2 || !blocks[1].IsError || blocks[1].Text != "falhou" {
		t.Errorf("tool com erro: %+v", blocks)
	}
}

func TestParseV2SchemaUnsupported(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "opencode.db")
	db, _ := sql.Open("sqlite", path)
	db.Exec("CREATE TABLE session_message(id TEXT); CREATE TABLE session_part(id TEXT)")
	db.Close()
	_, stats, err := parseAll(t, adapter(t, root))
	if !errors.Is(err, provider.ErrUnsupported) || !stats.Unsupported {
		t.Fatalf("err=%v stats=%+v", err, stats)
	}
}

func TestDetectAndOverride(t *testing.T) {
	root := t.TempDir()
	if d := adapter(t, root).Detect(context.Background()); d.Status != provider.StatusNotFound {
		t.Errorf("vazio: %+v", d)
	}
	os.MkdirAll(filepath.Join(root, "storage", "session"), 0o755)
	if d := adapter(t, root).Detect(context.Background()); d.Status != provider.StatusUnsupported {
		t.Errorf("legado: %+v", d)
	}
	buildDB(t, root)
	if d := adapter(t, root).Detect(context.Background()); d.Status != provider.StatusOK {
		t.Errorf("com banco: %+v", d)
	}

	other := t.TempDir()
	custom := buildDB(t, other)
	a := New(Options{RootDir: root, DBOverride: custom, SnapshotDir: t.TempDir()})
	srcs, _ := a.Sources(context.Background())
	if len(srcs) != 1 || srcs[0].Path != custom {
		t.Errorf("OPENCODE_DB absoluto: %+v", srcs)
	}
	a = New(Options{RootDir: root, DBOverride: ":memory:"})
	if srcs, _ := a.Sources(context.Background()); len(srcs) != 0 {
		t.Errorf(":memory: não tem arquivo: %+v", srcs)
	}
}

// Ler não pode criar nada no diretório do OpenCode.
func TestParseLeavesProviderDirUntouched(t *testing.T) {
	root := t.TempDir()
	buildDB(t, root)
	before, _ := os.ReadDir(root)
	if _, _, err := parseAll(t, adapter(t, root)); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadDir(root)
	if len(before) != len(after) {
		t.Errorf("arquivos: %d → %d", len(before), len(after))
	}
}

// Viewer: lê só a sessão pedida; anexos viram referências.
func TestParseSessionOnlyAndFiles(t *testing.T) {
	root := t.TempDir()
	buildDB(t, root,
		`INSERT INTO session (id, project_id, slug, directory, title, version, time_created, time_updated)
		 VALUES ('ses_b', 'p', 's', '/x', 'B', '1', 1768046500000, 1768046600000)`,
		`INSERT INTO message VALUES ('msg_b', 'ses_b', 1768046500000, 1768046500000, '{"role":"user"}')`,
		`INSERT INTO part VALUES ('prt_b1', 'msg_b', 'ses_b', 1, 1, '{"type":"file","mime":"image/png","filename":"tela.png","url":"data:image/png;base64,AAAA"}')`,
		`INSERT INTO part VALUES ('prt_b2', 'msg_b', 'ses_b', 2, 2, '{"type":"file","mime":"text/plain","filename":"notas.txt"}')`,
	)
	a := adapter(t, root)
	srcs, _ := a.Sources(context.Background())
	sink := providertest.NewSink()
	key := domain.SessionKey{Provider: domain.ProviderOpenCode, NativeID: "ses_b"}
	if _, err := a.ParseSession(context.Background(), srcs[0], key, sink); err != nil {
		t.Fatal(err)
	}
	if len(sink.Sessions) != 1 || sink.Sessions[0].Key != key {
		t.Fatalf("sessões: %+v", sink.Sessions)
	}
	bs := sink.Messages[key][0].Blocks
	if len(bs) != 2 || bs[0].Kind != domain.BlockImageRef || bs[0].Text != "tela.png" || bs[0].Language != "image/png" ||
		bs[1].Kind != domain.BlockFile || bs[1].Text != "notas.txt" {
		t.Errorf("anexos: %+v", bs)
	}
	var _ provider.SessionReader = a
}
