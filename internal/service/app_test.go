package service

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"sessionflow/internal/config"
	"sessionflow/internal/domain"
	"sessionflow/internal/indexer"
	"sessionflow/internal/provider"
	"sessionflow/internal/provider/claude"
	"sessionflow/internal/provider/opencode"
	"sessionflow/internal/provider/providertest"
	"sessionflow/internal/repository/sqlite"
)

var discard = slog.New(slog.NewTextHandler(io.Discard, nil))

func noEnv(string) string { return "" }

func openIndex(t *testing.T, paths config.Paths) *sql.DB {
	t.Helper()
	idx, _, err := sqlite.OpenIndex(context.Background(), paths.IndexDB(), discard)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { idx.Close() })
	return idx
}

// Integração: config → adaptadores → indexação → Status.
func TestStatusAfterScan(t *testing.T) {
	home := t.TempDir()
	claudeFile := filepath.Join(home, ".claude", "projects", "-p", "11111111-1111-4111-8111-111111111111.jsonl")
	os.MkdirAll(filepath.Dir(claudeFile), 0o700)
	data, _ := os.ReadFile(providertest.Fixture("claude", "session.jsonl"))
	os.WriteFile(claudeFile, data, 0o600)
	customOC := filepath.Join(t.TempDir(), "oc")
	os.MkdirAll(customOC, 0o700)

	paths, _ := config.ResolvePaths(noEnv, home)
	idx := openIndex(t, paths)

	cfg := config.Default()
	cfg.Providers[domain.ProviderCodex] = config.ProviderConfig{Enabled: false}
	cfg.Providers[domain.ProviderOpenCode] = config.ProviderConfig{Enabled: true, RootDir: customOC}
	penv := ProviderEnv{Config: cfg, Env: noEnv, Home: home, Paths: paths}

	providers := NewProviders(penv)
	if len(providers) != 2 {
		t.Fatalf("providers habilitados = %d", len(providers))
	}
	if oc, ok := providers[1].(*opencode.Adapter); !ok || oc.Detect(context.Background()).RootDir != customOC {
		t.Errorf("opencode deveria usar rootDir configurado")
	}
	if _, ok := providers[0].(*claude.Adapter); !ok {
		t.Errorf("ordem: %T", providers[0])
	}

	var notified atomic.Int32
	sync := NewSyncService(indexer.New(sqlite.NewIndexStore(idx), discard, providers...), nil, discard, func() { notified.Add(1) })
	svc := NewAppService(AppDeps{Version: "1.2.3", Providers: penv, Index: idx, Sync: sync, Logger: discard})

	st, err := svc.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.Providers[0].Status != StatusPending || st.Scan.FinishedAt != nil {
		t.Errorf("antes da indexação: %+v", st.Providers[0])
	}

	sync.Start()
	sync.wait()
	if notified.Load() != 1 {
		t.Errorf("notificações = %d", notified.Load())
	}

	st, err = svc.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.Version != "1.2.3" || st.Index.Sessions != 1 || st.Index.Projects != 1 || st.Scan.Running || st.Scan.FinishedAt == nil {
		t.Errorf("status: %+v", st)
	}
	byID := map[domain.ProviderID]ProviderStatus{}
	for _, p := range st.Providers {
		byID[p.ID] = p
	}
	if c := byID[domain.ProviderClaude]; c.Status != "ok" || c.Sessions != 1 || c.Sources != 1 || c.Name != "Claude Code" {
		t.Errorf("claude: %+v", c)
	}
	if c := byID[domain.ProviderCodex]; c.Status != string(provider.StatusDisabled) || c.Enabled {
		t.Errorf("codex: %+v", c)
	}
	if c := byID[domain.ProviderOpenCode]; c.Status != string(provider.StatusNotFound) || c.RootDir != customOC {
		t.Errorf("opencode: %+v", c)
	}
}

type slowRunner struct{ started chan struct{} }

func (r slowRunner) Run(ctx context.Context, _ indexer.RunOptions) indexer.Report {
	close(r.started)
	<-ctx.Done()
	return indexer.Report{}
}

// Shutdown cancela a indexação em andamento e não notifica.
func TestSyncStopCancels(t *testing.T) {
	r := slowRunner{started: make(chan struct{})}
	var notified atomic.Int32
	s := NewSyncService(r, nil, discard, func() { notified.Add(1) })
	s.ServiceStartup(context.Background(), application.ServiceOptions{})
	<-r.started
	if _, running := s.snapshot(); !running {
		t.Error("deveria estar rodando")
	}
	s.Start() // ignorado: já rodando
	done := make(chan struct{})
	go func() { s.ServiceShutdown(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown não cancelou")
	}
	if notified.Load() != 0 {
		t.Error("cancelado não deveria notificar")
	}
	// Depois do shutdown os bancos fecham: Start (binding) não pode rodar.
	s.Start()
	if _, running := s.snapshot(); running {
		t.Error("Start após shutdown iniciou indexação")
	}
}
