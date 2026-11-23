package indexer

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sessionflow/internal/domain"
	"sessionflow/internal/provider"
	"sessionflow/internal/provider/claude"
	"sessionflow/internal/provider/codex"
	"sessionflow/internal/provider/opencode"
	"sessionflow/internal/provider/providertest"
	"sessionflow/internal/repository/sqlite"
)

var discard = slog.New(slog.NewTextHandler(io.Discard, nil))

const (
	claudeID   = "11111111-1111-4111-8111-111111111111"
	codexID    = "22222222-2222-7222-8222-222222222222"
	opencodeID = "ses_fixture000001AAAAAAAAAAAAAA"
)

type env struct {
	claudeRoot, codexRoot, opencodeRoot string
	claudeFile, codexFile               string
	db                                  *sql.DB
	store                               *sqlite.IndexStore
	providers                           []provider.Provider
}

func copyFixture(t *testing.T, dst string, parts ...string) {
	t.Helper()
	data, err := os.ReadFile(providertest.Fixture(parts...))
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Dir(dst), 0o755)
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// setup monta os três providers com os fixtures, nos layouts reais.
func setup(t *testing.T) *env {
	t.Helper()
	base := t.TempDir()
	e := &env{
		claudeRoot:   filepath.Join(base, "claude"),
		codexRoot:    filepath.Join(base, "codex"),
		opencodeRoot: filepath.Join(base, "opencode"),
	}
	e.claudeFile = filepath.Join(e.claudeRoot, "projects", "-home-user-projects-demo-app", claudeID+".jsonl")
	copyFixture(t, e.claudeFile, "claude", "session.jsonl")
	e.codexFile = filepath.Join(e.codexRoot, "sessions", "2026", "01", "10", "rollout-2026-01-10T12-00-00-"+codexID+".jsonl")
	copyFixture(t, e.codexFile, "codex", "rollout.jsonl")

	os.MkdirAll(e.opencodeRoot, 0o755)
	schema, _ := os.ReadFile(providertest.Fixture("opencode", "schema-v1.sql"))
	odb, err := sql.Open("sqlite", filepath.Join(e.opencodeRoot, "opencode.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := odb.Exec(string(schema)); err != nil {
		t.Fatal(err)
	}
	odb.Close()

	db, _, err := sqlite.OpenIndex(context.Background(), filepath.Join(base, "data", "index.db"), discard)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	e.db = db
	e.store = sqlite.NewIndexStore(db)
	e.providers = []provider.Provider{
		claude.New(claude.Options{RootDir: e.claudeRoot}),
		codex.New(codex.Options{RootDir: e.codexRoot}),
		opencode.New(opencode.Options{RootDir: e.opencodeRoot, SnapshotDir: filepath.Join(base, "cache")}),
	}
	return e
}

func (e *env) run(t *testing.T, extra ...provider.Provider) Report {
	t.Helper()
	return New(e.store, discard, append(e.providers, extra...)...).Run(context.Background(), RunOptions{})
}

func (e *env) count(t *testing.T, q string, args ...any) int {
	t.Helper()
	var n int
	if err := e.db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

func find(rep Report, id domain.ProviderID) ProviderReport {
	for _, p := range rep.Providers {
		if p.Detection.Provider == id {
			return p
		}
	}
	return ProviderReport{}
}

func TestIndexAllProviders(t *testing.T) {
	e := setup(t)
	rep := e.run(t)

	for _, id := range domain.AllProviders() {
		p := find(rep, id)
		if p.Error != "" || p.Indexed != 1 || p.Sessions != 1 || p.Detection.Status != provider.StatusOK {
			t.Errorf("%s: %+v", id, p)
		}
	}
	if p := find(rep, domain.ProviderClaude); p.Partial != 1 {
		t.Errorf("claude deveria marcar a linha truncada: %+v", p)
	}

	// As três sessões estão no mesmo diretório: um projeto só.
	if n := e.count(t, "SELECT count(*) FROM projects"); n != 1 {
		t.Errorf("projetos = %d", n)
	}
	if n := e.count(t, "SELECT count(DISTINCT project_id) FROM sessions"); n != 1 {
		t.Errorf("sessões em projetos distintos: %d", n)
	}
	var title, model string
	var created, updated int64
	err := e.db.QueryRow(`SELECT title, model, created_at, updated_at FROM sessions WHERE provider='claude' AND native_id=?`, claudeID).
		Scan(&title, &model, &created, &updated)
	if err != nil || title != "Listar arquivos Go" || model != "claude-opus-5-5" || created >= updated {
		t.Errorf("claude: %q %q %d %d %v", title, model, created, updated, err)
	}

	// Busca: "arquivos" aparece no prompt de todos os providers; sem acento
	// também acha ("Encontrei" via resposta).
	if n := e.count(t, `SELECT count(DISTINCT m.session_id) FROM messages_fts f JOIN messages m ON m.id = f.rowid WHERE messages_fts MATCH 'arquivos'`); n != 3 {
		t.Errorf("busca 'arquivos' em %d sessões", n)
	}
	// Saída de ferramenta indexada; contexto injetado e reasoning não.
	if n := e.count(t, `SELECT count(*) FROM messages_fts WHERE messages_fts MATCH '"main.go"'`); n == 0 {
		t.Error("resultado de ferramenta deveria ser pesquisável")
	}
	for _, q := range []string{"environment_context", "Injected", "Preciso"} {
		if n := e.count(t, `SELECT count(*) FROM messages_fts WHERE messages_fts MATCH ?`, q); n != 0 {
			t.Errorf("%q não deveria estar no índice", q)
		}
	}
	if n := e.count(t, `SELECT count(*) FROM sync_state`); n != 3 {
		t.Errorf("sync_state = %d", n)
	}
}

func TestIndexIsIncremental(t *testing.T) {
	e := setup(t)
	e.run(t)
	msgs := e.count(t, "SELECT count(*) FROM messages")

	rep := e.run(t)
	for _, p := range rep.Providers {
		if p.Indexed != 0 || p.Unchanged != 1 {
			t.Errorf("%s deveria pular a fonte inalterada: %+v", p.Detection.Provider, p)
		}
	}
	if n := e.count(t, "SELECT count(*) FROM messages"); n != msgs {
		t.Errorf("mensagens duplicadas: %d → %d", msgs, n)
	}

	// Arquivo cresce (a linha truncada é completada): só ele é relido.
	f, _ := os.OpenFile(e.claudeFile, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(`ente"},"type":"user","uuid":"u9","timestamp":"2026-01-10T12:01:00.000Z","sessionId":"` + claudeID + `"}` + "\n")
	f.Close()
	rep = e.run(t)
	if p := find(rep, domain.ProviderClaude); p.Indexed != 1 || p.Partial != 0 || p.BadRecords != 0 {
		t.Errorf("claude após append: %+v", p)
	}
	if find(rep, domain.ProviderCodex).Indexed != 0 {
		t.Error("codex não mudou e foi relido")
	}
	if n := e.count(t, `SELECT count(*) FROM messages_fts WHERE messages_fts MATCH 'propositalmente'`); n != 1 {
		t.Errorf("linha completada não indexada: %d", n)
	}
	if n := e.count(t, "SELECT count(*) FROM sessions"); n != 3 {
		t.Errorf("sessões = %d", n)
	}
}

func TestIndexMirrorsDeletedSources(t *testing.T) {
	e := setup(t)
	e.run(t)
	os.Remove(e.codexFile)
	rep := e.run(t)
	if p := find(rep, domain.ProviderCodex); p.Removed != 1 || p.Sessions != 0 {
		t.Errorf("codex: %+v", p)
	}
	if n := e.count(t, "SELECT count(*) FROM sync_state WHERE provider='codex'"); n != 0 {
		t.Errorf("sync_state do codex = %d", n)
	}
	// O diretório inteiro some: provider não encontrado, sessões removidas.
	os.RemoveAll(e.claudeRoot)
	rep = e.run(t)
	if p := find(rep, domain.ProviderClaude); p.Detection.Status != provider.StatusNotFound || p.Sessions != 0 {
		t.Errorf("claude: %+v", p)
	}
	if n := e.count(t, "SELECT count(*) FROM projects"); n != 1 {
		t.Errorf("projeto do opencode deveria continuar: %d", n)
	}
}

// Parse com erro mantém a leitura anterior da fonte e não para os outros.
func TestFailedSourceKeepsPreviousData(t *testing.T) {
	e := setup(t)
	e.run(t)
	// Rollout reescrito sem ID e com nome fora do padrão não existe aqui;
	// simula erro de leitura tirando a permissão.
	if os.Getuid() == 0 {
		t.Skip("root ignora permissões")
	}
	os.Chmod(e.codexFile, 0)
	t.Cleanup(func() { os.Chmod(e.codexFile, 0o644) })
	future := time.Now().Add(time.Hour)
	os.Chtimes(e.codexFile, future, future)

	rep := e.run(t)
	if p := find(rep, domain.ProviderCodex); p.Failed != 1 || p.Sessions != 1 {
		t.Errorf("codex: %+v", p)
	}
	var status, msg string
	e.db.QueryRow("SELECT status, error FROM sync_state WHERE provider='codex'").Scan(&status, &msg)
	if status != "error" || !strings.Contains(msg, "permission denied") {
		t.Errorf("estado: %q %q", status, msg)
	}
	if find(rep, domain.ProviderOpenCode).Error != "" {
		t.Error("opencode afetado")
	}
	// Erro transitório: corrigida a permissão, sem mudar tamanho nem data,
	// a fonte é relida na próxima execução.
	os.Chmod(e.codexFile, 0o644)
	rep = e.run(t)
	if p := find(rep, domain.ProviderCodex); p.Indexed != 1 || p.Failed != 0 {
		t.Errorf("fonte com erro deveria ser relida: %+v", p)
	}
	e.db.QueryRow("SELECT status FROM sync_state WHERE provider='codex'").Scan(&status)
	if status != "ok" {
		t.Errorf("estado após nova leitura: %q", status)
	}
}

// Diretório ilegível não pode apagar as sessões dele do índice.
func TestIncompleteListingKeepsSessions(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root ignora permissões")
	}
	e := setup(t)
	e.run(t)
	dir := filepath.Dir(e.claudeFile)
	os.Chmod(dir, 0)
	t.Cleanup(func() { os.Chmod(dir, 0o755) })

	rep := e.run(t)
	p := find(rep, domain.ProviderClaude)
	if p.Removed != 0 || p.Sessions != 1 || p.Error != "" || !strings.Contains(p.Detection.Detail, "ilegível") {
		t.Errorf("claude: %+v", p)
	}
}

func TestDisabledProviderIsPurged(t *testing.T) {
	e := setup(t)
	e.run(t)
	// Codex desabilitado: fica fora da lista e entra em WithDisabled.
	enabled := []provider.Provider{e.providers[0], e.providers[2]}
	New(e.store, discard, enabled...).WithDisabled(domain.ProviderCodex).Run(context.Background(), RunOptions{})
	if n := e.count(t, "SELECT count(*) FROM sessions WHERE provider='codex'"); n != 0 {
		t.Errorf("sessões do codex desabilitado: %d", n)
	}
	if n := e.count(t, "SELECT count(*) FROM sessions"); n != 2 {
		t.Errorf("outros providers afetados: %d", n)
	}
}

// Mesma sessão em duas fontes: a segunda fica "duplicate" e assume quando
// a primeira some, sem precisar mudar.
func TestDuplicateSourceTakesOver(t *testing.T) {
	e := setup(t)
	data, _ := os.ReadFile(e.claudeFile)
	other := filepath.Join(e.claudeRoot, "projects", "-outro-dir", claudeID+".jsonl")
	os.MkdirAll(filepath.Dir(other), 0o755)
	os.WriteFile(other, data, 0o644)

	rep := e.run(t)
	if p := find(rep, domain.ProviderClaude); p.Duplicates != 1 || p.Sessions != 1 {
		t.Fatalf("claude: %+v", p)
	}
	var winner string
	e.db.QueryRow("SELECT source_path FROM sessions WHERE provider='claude'").Scan(&winner)
	loser := other
	if winner == other {
		loser = e.claudeFile
	}
	os.Remove(winner)
	e.run(t)
	var now string
	e.db.QueryRow("SELECT source_path FROM sessions WHERE provider='claude'").Scan(&now)
	if now != loser {
		t.Errorf("sessão deveria vir de %s, veio %q", loser, now)
	}
}

// fake é um provider com defeito controlado.
type fake struct {
	id      domain.ProviderID
	panicOn string // "detect", "sources", "parse"
	err     error
}

func (f fake) ID() domain.ProviderID { return f.id }
func (f fake) Detect(context.Context) provider.Detection {
	if f.panicOn == "detect" {
		panic("conteúdo secreto")
	}
	return provider.Detection{Provider: f.id, Status: provider.StatusOK}
}
func (f fake) Sources(context.Context) ([]provider.Source, error) {
	if f.panicOn == "sources" {
		panic(errors.New("panic com error"))
	}
	if f.err != nil {
		return nil, f.err
	}
	return []provider.Source{{Path: "/fake/a"}}, nil
}
func (f fake) Parse(context.Context, provider.Source, provider.Sink) (provider.ParseStats, error) {
	if f.panicOn == "parse" {
		panic("boom")
	}
	return provider.ParseStats{}, nil
}

func TestProviderFailuresAreIsolated(t *testing.T) {
	for _, mode := range []string{"detect", "sources", "parse", "error"} {
		t.Run(mode, func(t *testing.T) {
			e := setup(t)
			f := fake{id: "fake", panicOn: mode}
			if mode == "error" {
				f = fake{id: "fake", err: errors.New("falhou")}
			}
			// O defeituoso roda primeiro; os reais precisam indexar mesmo assim.
			e.providers = append([]provider.Provider{f}, e.providers...)
			rep := e.run(t)
			bad := rep.Providers[0]
			// panic no parse afeta só a fonte (Failed); nos demais, o provider.
			if len(rep.Providers) != 4 || (bad.Error == "" && bad.Failed != 1) {
				t.Fatalf("relatório: %+v", rep.Providers)
			}
			if strings.Contains(rep.Providers[0].Error, "secreto") {
				t.Error("valor do panic vazou para o relatório")
			}
			if n := e.count(t, "SELECT count(*) FROM sessions"); n != 3 {
				t.Errorf("sessões = %d", n)
			}
		})
	}
}

func TestRunStopsOnCancel(t *testing.T) {
	e := setup(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rep := New(e.store, discard, e.providers...).Run(ctx, RunOptions{})
	if len(rep.Providers) != 0 {
		t.Errorf("não deveria rodar: %+v", rep.Providers)
	}
}

func TestSearchText(t *testing.T) {
	m := domain.Message{Role: domain.RoleAssistant, Blocks: []domain.Block{
		{Kind: domain.BlockText, Text: "olá"},
		{Kind: domain.BlockReasoning, Text: "segredo"},
		{Kind: domain.BlockToolCall, ToolName: "Bash", ToolInput: []byte(`{"command":"ls"}`)},
		{Kind: domain.BlockToolResult, Text: strings.Repeat("x", 10_000)},
		{Kind: domain.BlockUnknown, Raw: []byte(`{"a":1}`)},
	}}
	got := SearchText(m)
	if !strings.HasPrefix(got, "olá\nBash {\"command\":\"ls\"}\n") || strings.Contains(got, "segredo") || len(got) > 5000 {
		t.Errorf("SearchText = %.80q… (%d bytes)", got, len(got))
	}
	if SearchText(domain.Message{Role: domain.RoleSystem, Blocks: []domain.Block{{Kind: domain.BlockText, Text: "x"}}}) != "" {
		t.Error("system não é indexado")
	}
}

// Execução parcial roda só os providers pedidos e não remove os
// desabilitados.
func TestRunOnlyRequestedProviders(t *testing.T) {
	e := setup(t)
	rep := New(e.store, discard, e.providers...).Run(context.Background(), RunOptions{Providers: []domain.ProviderID{domain.ProviderCodex}})
	if len(rep.Providers) != 1 || rep.Providers[0].Detection.Provider != domain.ProviderCodex || !rep.Changed() {
		t.Fatalf("parcial: %+v", rep.Providers)
	}
	if n := e.count(t, "SELECT count(*) FROM sessions WHERE provider != 'codex'"); n != 0 {
		t.Errorf("outros providers indexados: %d", n)
	}
	if rep := New(e.store, discard, e.providers...).Run(context.Background(), RunOptions{Providers: []domain.ProviderID{domain.ProviderCodex}}); rep.Changed() {
		t.Error("sem mudança, mas Changed")
	}
}

// Fonte grande relida há pouco é adiada nas execuções do watcher, nunca
// nas completas.
func TestThrottleDefersLargeRecentSource(t *testing.T) {
	e := setup(t)
	line := `{"type":"user","timestamp":"2026-01-01T00:00:00Z","cwd":"/p","message":{"content":"` + strings.Repeat("x", 1000) + `"}}` + "\n"
	big := filepath.Join(e.claudeRoot, "projects", "-p", "44444444-4444-4444-8444-444444444444.jsonl")
	os.MkdirAll(filepath.Dir(big), 0o755)
	os.WriteFile(big, []byte(strings.Repeat(line, ThrottleSize/len(line)+10)), 0o644)

	ix := New(e.store, discard, e.providers[0])
	now := time.Now()
	ix.now = func() time.Time { return now }
	claudeOnly := RunOptions{Providers: []domain.ProviderID{domain.ProviderClaude}, Throttle: true}
	if rep := ix.Run(context.Background(), claudeOnly); rep.Providers[0].Indexed != 2 || rep.Providers[0].Deferred != 0 {
		t.Fatalf("1ª leitura: %+v", rep.Providers[0])
	}
	f, _ := os.OpenFile(big, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(line)
	f.Close()

	if rep := ix.Run(context.Background(), claudeOnly); rep.Providers[0].Deferred != 1 || rep.Providers[0].Indexed != 0 || rep.Changed() {
		t.Errorf("adiada: %+v", rep.Providers[0])
	}
	if ids := ix.Run(context.Background(), claudeOnly).Deferred(); len(ids) != 1 || ids[0] != domain.ProviderClaude {
		t.Errorf("Deferred(): %v", ids)
	}
	now = now.Add(ThrottleEvery + time.Second)
	if rep := ix.Run(context.Background(), claudeOnly); rep.Providers[0].Indexed != 1 || rep.Providers[0].Deferred != 0 {
		t.Errorf("após o intervalo: %+v", rep.Providers[0])
	}
	f, _ = os.OpenFile(big, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(line)
	f.Close()
	if rep := ix.Run(context.Background(), RunOptions{}); rep.Providers[0].Indexed != 1 {
		t.Errorf("completa não adia: %+v", rep.Providers[0])
	}
}

// has_text segue domain.CountsAsMessage (a soma bate com message_count) e
// tools guarda as ferramentas chamadas em cada mensagem.
func TestIndexerFillsHasTextAndTools(t *testing.T) {
	e := setup(t)
	e.run(t)
	if n := e.count(t, "SELECT count(*) FROM messages WHERE has_text = 1"); n == 0 {
		t.Fatal("nenhuma mensagem com texto")
	}
	if n := e.count(t, `SELECT count(*) FROM sessions s WHERE s.message_count !=
		(SELECT coalesce(sum(m.has_text), 0) FROM messages m WHERE m.session_id = s.id)`); n != 0 {
		t.Errorf("%d sessões com has_text diferente de message_count", n)
	}
	if n := e.count(t, "SELECT count(*) FROM messages WHERE tools != '[]' AND has_text = 0"); n == 0 {
		t.Error("nenhum passo só de ferramenta com tools preenchido")
	}
	if n := e.count(t, "SELECT count(*) FROM messages m, json_each(m.tools) j WHERE j.value = ''"); n != 0 {
		t.Error("nome de ferramenta vazio")
	}
	if n := e.count(t, "SELECT count(*) FROM messages WHERE json_valid(tools) = 0"); n != 0 {
		t.Error("tools não é JSON")
	}
}
