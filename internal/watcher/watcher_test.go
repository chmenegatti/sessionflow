package watcher

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"sessionflow/internal/domain"
	"sessionflow/internal/provider"
)

var discard = slog.New(slog.NewTextHandler(io.Discard, nil))

type recorder struct {
	mu    sync.Mutex
	calls [][]domain.ProviderID
}

func (r *recorder) onChange(ids []domain.ProviderID) {
	r.mu.Lock()
	r.calls = append(r.calls, ids)
	r.mu.Unlock()
}

func (r *recorder) take() [][]domain.ProviderID {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.calls
	r.calls = nil
	return out
}

// wait espera o primeiro aviso (ou o prazo) e devolve os acumulados.
func (r *recorder) wait(d time.Duration) [][]domain.ProviderID {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		n := len(r.calls)
		r.mu.Unlock()
		if n > 0 {
			time.Sleep(60 * time.Millisecond) // avisos em sequência
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	return r.take()
}

const quiet = 50 * time.Millisecond

type env struct {
	w        *Watcher
	rec      *recorder
	claude   string // raiz recursiva
	opencode string // raiz não recursiva
}

func start(t *testing.T, createOC bool) *env {
	t.Helper()
	dir := t.TempDir()
	e := &env{rec: &recorder{}, claude: filepath.Join(dir, "claude", "projects"), opencode: filepath.Join(dir, "opencode")}
	os.MkdirAll(filepath.Join(e.claude, "-p"), 0o755)
	if createOC {
		os.MkdirAll(e.opencode, 0o755)
	}
	e.w = New([]Target{
		{Provider: domain.ProviderClaude, Dirs: []provider.WatchDir{{Path: e.claude, Recursive: true}}},
		{Provider: domain.ProviderOpenCode, Dirs: []provider.WatchDir{{Path: e.opencode}}},
	}, e.rec.onChange, nil, discard, Options{Quiet: quiet, MaxWait: 300 * time.Millisecond})
	if err := e.w.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.w.Close() })
	return e
}

func write(t *testing.T, path, s string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(s)
	f.Close()
}

func only(calls [][]domain.ProviderID, id domain.ProviderID) bool {
	return len(calls) == 1 && slices.Equal(calls[0], []domain.ProviderID{id})
}

func TestWatcherFileLifecycle(t *testing.T) {
	e := start(t, true)
	file := filepath.Join(e.claude, "-p", "s.jsonl")

	write(t, file, "{}\n") // criação
	if c := e.rec.wait(time.Second); !only(c, domain.ProviderClaude) {
		t.Fatalf("criação: %v", c)
	}
	for range 20 { // rajada de escritas = um aviso
		write(t, file, "{}\n")
	}
	if c := e.rec.wait(time.Second); !only(c, domain.ProviderClaude) {
		t.Errorf("rajada: %v", c)
	}
	os.Remove(file) // exclusão
	if c := e.rec.wait(time.Second); !only(c, domain.ProviderClaude) {
		t.Errorf("exclusão: %v", c)
	}
	// Arquivos que não são fontes não avisam.
	write(t, filepath.Join(e.claude, "-p", "notas.txt"), "x")
	write(t, filepath.Join(e.opencode, "opencode.db-shm"), "x")
	write(t, filepath.Join(e.opencode, "auth.json"), "x")
	if c := e.rec.wait(200 * time.Millisecond); len(c) != 0 {
		t.Errorf("irrelevantes avisaram: %v", c)
	}
	// Banco e WAL do OpenCode avisam; providers diferentes saem juntos.
	write(t, filepath.Join(e.opencode, "opencode.db-wal"), "x")
	write(t, filepath.Join(e.claude, "-p", "t.jsonl"), "x")
	if c := e.rec.wait(time.Second); len(c) != 1 || !slices.Equal(c[0], []domain.ProviderID{domain.ProviderClaude, domain.ProviderOpenCode}) {
		t.Errorf("dois providers: %v", c)
	}
}

// Pasta nova (projeto novo, subagentes) passa a ser monitorada, e arquivos
// dentro dela avisam.
func TestWatcherNewDirectories(t *testing.T) {
	e := start(t, true)
	nested := filepath.Join(e.claude, "-novo", "uuid", "subagents")
	os.MkdirAll(nested, 0o755)
	if c := e.rec.wait(time.Second); !only(c, domain.ProviderClaude) {
		t.Fatalf("pasta nova: %v", c)
	}
	write(t, filepath.Join(nested, "agent-a.jsonl"), "{}\n")
	if c := e.rec.wait(time.Second); !only(c, domain.ProviderClaude) {
		t.Errorf("arquivo na pasta nova: %v", c)
	}
	// Pasta removida sai do monitor (e avisa: as sessões dela sumiram).
	os.RemoveAll(filepath.Join(e.claude, "-novo"))
	if c := e.rec.wait(time.Second); len(c) == 0 {
		t.Error("remoção de pasta não avisou")
	}
	e.w.mu.Lock()
	for p := range e.w.watched {
		if filepath.Base(filepath.Dir(p)) == "-novo" || filepath.Base(p) == "-novo" {
			t.Errorf("pasta removida ainda no mapa: %s", p)
		}
	}
	e.w.mu.Unlock()
	// Raiz não recursiva não entra em subpastas.
	os.Mkdir(filepath.Join(e.opencode, "storage"), 0o755)
	write(t, filepath.Join(e.opencode, "storage", "x.db"), "x")
	if c := e.rec.wait(200 * time.Millisecond); len(c) != 0 {
		t.Errorf("subpasta de raiz não recursiva: %v", c)
	}
}

// Provider instalado depois: Refresh passa a monitorar a raiz.
func TestWatcherRefreshAddsLateRoot(t *testing.T) {
	e := start(t, false)
	os.MkdirAll(e.opencode, 0o755)
	write(t, filepath.Join(e.opencode, "opencode.db"), "x")
	if c := e.rec.wait(200 * time.Millisecond); len(c) != 0 {
		t.Fatalf("raiz ainda não monitorada avisou: %v", c)
	}
	e.w.Refresh()
	write(t, filepath.Join(e.opencode, "opencode.db"), "y")
	if c := e.rec.wait(time.Second); !only(c, domain.ProviderOpenCode) {
		t.Errorf("após Refresh: %v", c)
	}
	if e.w.Degraded() {
		t.Error("degradado sem motivo")
	}
}

// Escrita contínua não segura o aviso para sempre (MaxWait).
func TestWatcherMaxWait(t *testing.T) {
	e := start(t, true)
	file := filepath.Join(e.claude, "-p", "s.jsonl")
	stop := time.Now().Add(700 * time.Millisecond)
	for time.Now().Before(stop) {
		write(t, file, "{}\n")
		time.Sleep(quiet / 3)
	}
	if c := e.rec.take(); len(c) < 2 {
		t.Errorf("avisos durante escrita contínua: %d", len(c))
	}
}

func TestWatcherCloseIsIdempotent(t *testing.T) {
	e := start(t, true)
	if err := e.w.Close(); err != nil {
		t.Fatal(err)
	}
	e.w.Close()
	e.w.Refresh() // depois de fechado: nada
	if !e.w.Degraded() {
		t.Error("fechado deveria contar como degradado")
	}
}
