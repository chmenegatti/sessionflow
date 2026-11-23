package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sessionflow/internal/provider/providertest"
	"strings"
	"testing"
	"time"

	"sessionflow/internal/config"
	"sessionflow/internal/domain"
	"sessionflow/internal/indexer"
	"sessionflow/internal/provider"
	"sessionflow/internal/provider/claude"
	"sessionflow/internal/repository/sqlite"
	"sessionflow/internal/terminal"
	"sessionflow/internal/watcher"
)

func newSettings(t *testing.T, loadErr error) (*SettingsService, string, string) {
	t.Helper()
	dir := t.TempDir()
	paths := config.Paths{ConfigDir: dir, DataDir: dir, CacheDir: dir, StateDir: dir}
	cli := providertest.Exe(dir, "claude-bin")
	os.WriteFile(cli, nil, 0o755)
	store := NewConfigStore(paths.ConfigFile(), config.Default(), loadErr)
	env := ProviderEnv{Config: config.Default(), Env: noEnv, Home: dir, Paths: paths}
	look := func(string) (string, error) { return "", errors.New("não") }
	providers := []provider.Provider{claude.New(claude.Options{Home: dir, LookPath: look})}
	return NewSettingsService(store, env, providers, look), dir, cli
}

func TestSettingsCLIPath(t *testing.T) {
	s, dir, cli := newSettings(t, nil)
	ctx := context.Background()
	if v := s.Get(ctx); v.Providers[0].DetectedCLI != "" || (v.Terminal.Error == "") != consoleFallback || !v.Writable {
		t.Fatalf("inicial: %+v", v)
	}
	plain := filepath.Join(dir, "plain")
	os.WriteFile(plain, nil, 0o644)
	for path, want := range map[string]error{"rel/claude": ErrPathNotAbsolute, plain: ErrNotExecutable, dir: ErrNotExecutable} {
		if err := s.SetCLIPath(ctx, domain.ProviderClaude, path); !errors.Is(err, want) {
			t.Errorf("%s: %v", path, err)
		}
	}
	if err := s.SetCLIPath(ctx, domain.ProviderClaude, " "+cli+" "); err != nil {
		t.Fatal(err)
	}
	// Vale na hora e fica gravado.
	if v := s.Get(ctx); v.Providers[0].CLIPath != cli || v.Providers[0].DetectedCLI != cli || v.RestartRequired {
		t.Errorf("após salvar: %+v", v.Providers[0])
	}
	saved, err := config.Load(filepath.Join(dir, "config.json"))
	if err != nil || saved.Providers[domain.ProviderClaude].CLIPath != cli {
		t.Errorf("arquivo: %+v %v", saved.Providers, err)
	}
	if err := s.SetCLIPath(ctx, "nope", cli); err == nil {
		t.Error("provider inválido aceito")
	}
}

func TestSettingsProviderNeedsRestart(t *testing.T) {
	s, dir, _ := newSettings(t, nil)
	ctx := context.Background()
	if err := s.SetProvider(ctx, domain.ProviderCodex, true, filepath.Join(dir, "nao-existe")); !errors.Is(err, ErrNotDirectory) {
		t.Errorf("raiz ausente: %v", err)
	}
	if err := s.SetProvider(ctx, domain.ProviderCodex, false, dir); err != nil {
		t.Fatal(err)
	}
	v := s.Get(ctx)
	if !v.RestartRequired || v.Providers[1].Enabled || v.Providers[1].RootDir != dir {
		t.Errorf("%+v", v)
	}
}

func TestSettingsTerminal(t *testing.T) {
	s, dir, cli := newSettings(t, nil)
	ctx := context.Background()
	if err := s.SetTerminal(ctx, providertest.Abs("nao", "existe"), nil); !errors.Is(err, terminal.ErrInvalidTerminal) {
		t.Errorf("inexistente: %v", err)
	}
	if err := s.SetTerminal(ctx, cli, []string{"-e"}); !errors.Is(err, terminal.ErrInvalidTemplate) {
		t.Errorf("sem {cmd}: %v", err)
	}
	if err := s.SetTerminal(ctx, "", []string{"{cmd}"}); err == nil {
		t.Error("args sem caminho aceitos")
	}
	if err := s.SetTerminal(ctx, cli, []string{"--dir={dir}", " ", "{cmd}"}); err != nil {
		t.Fatal(err)
	}
	if v := s.Get(ctx); v.Terminal.Resolved != cli || len(v.Terminal.Args) != 2 || v.Terminal.Error != "" {
		t.Errorf("salvo: %+v", v.Terminal)
	}
	if err := s.SetTerminal(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	if v := s.Get(ctx); v.Terminal.Path != "" || strings.Contains(v.Terminal.Error, "nenhum") == consoleFallback {
		t.Errorf("volta a detectar: %+v", v.Terminal)
	}
	_ = dir
}

// config.json ilegível: a interface não grava por cima.
func TestSettingsRefusesToOverwriteBrokenConfig(t *testing.T) {
	s, _, cli := newSettings(t, errors.New("malformado"))
	if v := s.Get(context.Background()); v.Writable {
		t.Error("deveria ser só leitura")
	}
	if err := s.SetCLIPath(context.Background(), domain.ProviderClaude, cli); !errors.Is(err, ErrConfigUnreadable) {
		t.Errorf("%v", err)
	}
}

// Ponta a ponta: arquivo criado, alterado e apagado no diretório do
// provider chega ao índice (e à busca) sem ninguém pedir, pelo watcher.
func TestWatcherKeepsIndexInSync(t *testing.T) {
	root := t.TempDir()
	projects := filepath.Join(root, "projects")
	os.MkdirAll(filepath.Join(projects, "-p"), 0o755)
	db, _, err := sqlite.OpenIndex(context.Background(), filepath.Join(t.TempDir(), "index.db"), discard)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	store := sqlite.NewIndexStore(db)
	cl := claude.New(claude.Options{RootDir: root})
	search := NewSearchService(store, discard)

	updates := make(chan struct{}, 16)
	sync := NewWatchedSync(indexer.New(store, discard, cl), discard, func() { updates <- struct{}{} },
		func(changed func([]domain.ProviderID), lost func()) Watcher {
			return watcher.New([]watcher.Target{{Provider: domain.ProviderClaude, Dirs: cl.WatchDirs()}}, changed, lost, discard,
				watcher.Options{Quiet: 30 * time.Millisecond, MaxWait: 200 * time.Millisecond})
		})
	sync.ServiceStartup(context.Background(), applicationOptions())
	t.Cleanup(func() { sync.ServiceShutdown() })
	<-updates // indexação inicial (vazia)

	hits := func(q string) int {
		r, err := search.Search(context.Background(), SearchQuery{Text: q})
		if err != nil {
			t.Fatal(err)
		}
		return len(r.Hits)
	}
	waitFor := func(what string, ok func() bool) {
		t.Helper()
		deadline := time.After(5 * time.Second)
		for !ok() {
			select {
			case <-updates:
			case <-deadline:
				t.Fatalf("%s: índice não acompanhou", what)
			}
		}
	}
	file := filepath.Join(projects, "-p", bigID+".jsonl")
	line := func(s string) string {
		return `{"type":"user","timestamp":"2026-01-01T00:00:00Z","cwd":"/p","message":{"content":"` + s + `"}}` + "\n"
	}
	os.WriteFile(file, []byte(line("morango")), 0o644)
	waitFor("criação", func() bool { return hits("morango") == 1 })

	f, _ := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(line("framboesa"))
	f.Close()
	waitFor("acréscimo", func() bool { return hits("framboesa") == 1 && hits("morango") == 1 })

	// Projeto novo (pasta criada depois do início).
	os.MkdirAll(filepath.Join(projects, "-q"), 0o755)
	os.WriteFile(filepath.Join(projects, "-q", "55555555-5555-4555-8555-555555555555.jsonl"), []byte(line("amora")), 0o644)
	waitFor("projeto novo", func() bool { return hits("amora") == 1 })

	os.Remove(file)
	waitFor("exclusão", func() bool { return hits("morango") == 0 && hits("framboesa") == 0 })
}

// consoleFallback: no Windows sempre há onde retomar (console nova), então
// "sem terminal" não é erro.
var consoleFallback = runtime.GOOS == "windows"
