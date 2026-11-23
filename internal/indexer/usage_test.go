package indexer_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sessionflow/internal/domain"
	"sessionflow/internal/indexer"
	"sessionflow/internal/indexer/indexertest"
	"sessionflow/internal/provider"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

const sums = `SELECT count(*) || ':' || coalesce(sum(input_tokens),0) || ':' || coalesce(sum(output_tokens),0) || ':' ||
	coalesce(sum(cache_read_tokens),0) || ':' || coalesce(sum(total_tokens),0) || ':' || count(total_tokens) FROM usage`

func snapshot(t *testing.T, e *indexertest.UsageEnv) string {
	t.Helper()
	var s string
	if err := e.DB.QueryRow(sums).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestReindexIdempotent(t *testing.T) {
	e := indexertest.NewUsageEnv(t)
	e.Run(t)
	first := snapshot(t, e)
	// Registros por sessão antes da deduplicação entre sessões: Claude 8
	// (principal 3, a1 2, a2 1, automática 1, órfão 1), Codex 3, OpenCode 3.
	if !strings.HasPrefix(first, "14:") {
		t.Fatalf("primeira indexação: %s", first)
	}
	if _, err := e.DB.Exec("DELETE FROM sync_state"); err != nil {
		t.Fatal(err)
	}
	e.Run(t)
	if again := snapshot(t, e); again != first {
		t.Errorf("releitura mudou os registros: %s → %s", first, again)
	}
}

// failing faz o parse falhar no meio da fonte (depois do primeiro registro
// de consumo), como uma interrupção.
type failing struct{ provider.Provider }

type failSink struct {
	provider.Sink
	us provider.UsageSink
	n  int
}

func (s *failSink) Usage(k domain.SessionKey, u domain.Usage) error {
	if s.n++; s.n > 1 {
		return errors.New("interrompido")
	}
	return s.us.Usage(k, u)
}

func (f failing) Parse(ctx context.Context, src provider.Source, sink provider.Sink) (provider.ParseStats, error) {
	return f.Provider.Parse(ctx, src, &failSink{Sink: sink, us: sink.(provider.UsageSink)})
}

func TestReindexRecoversAfterFailure(t *testing.T) {
	e := indexertest.NewUsageEnv(t)
	codexOnly := e.Providers[1]
	indexer.New(e.Store, quiet, failing{codexOnly}).Run(context.Background(), indexer.RunOptions{})
	if n := e.Count(t, "SELECT count(*) FROM usage"); n != 0 {
		t.Fatalf("%d registros gravados de uma fonte que falhou", n)
	}
	if n := e.Count(t, "SELECT count(*) FROM sync_state WHERE status = 'error'"); n == 0 {
		t.Error("falha não registrada no estado")
	}
	indexer.New(e.Store, quiet, codexOnly).Run(context.Background(), indexer.RunOptions{})
	if n := e.Count(t, "SELECT count(*) FROM usage"); n != 3 {
		t.Errorf("%d registros após recuperar; quer 3", n)
	}
}

func TestReindexGrowingFileNoDuplicate(t *testing.T) {
	e := indexertest.NewUsageEnv(t)
	main := filepath.Join(e.ClaudeRoot, "projects", "-home-user-projects-demo-app", "33333333-3333-4333-8333-333333333333.jsonl")
	full, err := os.ReadFile(main)
	if err != nil {
		t.Fatal(err)
	}
	// Corta no meio da linha de msg_FIX_B (escrita em andamento).
	cut := strings.Index(string(full), `"msg_FIX_B"`)
	os.WriteFile(main, full[:cut], 0o644)
	claudeOnly := e.Providers[0]
	indexer.New(e.Store, quiet, claudeOnly).Run(context.Background(), indexer.RunOptions{})
	q := "SELECT count(*) FROM usage u JOIN sessions s ON s.id = u.session_id WHERE s.native_id = '33333333-3333-4333-8333-333333333333'"
	if n := e.Count(t, q); n != 1 {
		t.Fatalf("parcial: %d registros; quer 1 (msg_FIX_A)", n)
	}
	os.WriteFile(main, full, 0o644)
	later := time.Now().Add(time.Minute)
	os.Chtimes(main, later, later)
	indexer.New(e.Store, quiet, claudeOnly).Run(context.Background(), indexer.RunOptions{})
	if n := e.Count(t, q); n != 3 {
		t.Errorf("completo: %d registros; quer 3 (A, B, C uma vez cada)", n)
	}
}
