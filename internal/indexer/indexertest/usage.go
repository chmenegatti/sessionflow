// Package indexertest monta índices de teste a partir dos fixtures, com os
// adaptadores e o indexador reais.
package indexertest

import (
	"context"
	"database/sql"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"sessionflow/internal/indexer"
	"sessionflow/internal/provider"
	"sessionflow/internal/provider/claude"
	"sessionflow/internal/provider/codex"
	"sessionflow/internal/provider/opencode"
	"sessionflow/internal/provider/providertest"
	"sessionflow/internal/repository/sqlite"

	_ "modernc.org/sqlite"
)

var discard = slog.New(slog.NewTextHandler(io.Discard, nil))

// UsageEnv é um índice com os fixtures de consumo (docs/discovery/fixtures/usage).
type UsageEnv struct {
	ClaudeRoot, CodexRoot, OpenCodeRoot string
	DB                                  *sql.DB
	Store                               *sqlite.IndexStore
	Providers                           []provider.Provider
}

// NewUsageEnv copia os fixtures para os layouts reais e abre um índice vazio.
func NewUsageEnv(t *testing.T) *UsageEnv {
	t.Helper()
	base := t.TempDir()
	e := &UsageEnv{
		ClaudeRoot:   filepath.Join(base, "claude"),
		CodexRoot:    filepath.Join(base, "codex"),
		OpenCodeRoot: filepath.Join(base, "opencode"),
	}
	copyTree(t, providertest.Fixture("usage", "claude"), filepath.Join(e.ClaudeRoot, "projects"))
	copyTree(t, providertest.Fixture("usage", "codex"), filepath.Join(e.CodexRoot, "sessions"))
	os.MkdirAll(e.OpenCodeRoot, 0o755)
	q, err := os.ReadFile(providertest.Fixture("usage", "opencode", "usage.sql"))
	if err != nil {
		t.Fatal(err)
	}
	odb, err := sql.Open("sqlite", filepath.Join(e.OpenCodeRoot, "opencode.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := odb.Exec(string(q)); err != nil {
		t.Fatal(err)
	}
	odb.Close()

	db, _, err := sqlite.OpenIndex(context.Background(), filepath.Join(base, "data", "index.db"), discard)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	e.DB, e.Store = db, sqlite.NewIndexStore(db)
	e.Providers = []provider.Provider{
		claude.New(claude.Options{RootDir: e.ClaudeRoot}),
		codex.New(codex.Options{RootDir: e.CodexRoot}),
		opencode.New(opencode.Options{RootDir: e.OpenCodeRoot, SnapshotDir: filepath.Join(base, "cache")}),
	}
	return e
}

// Run indexa com os adaptadores reais.
func (e *UsageEnv) Run(t *testing.T) indexer.Report {
	t.Helper()
	return indexer.New(e.Store, discard, e.Providers...).Run(context.Background(), indexer.RunOptions{})
}

// Count executa uma consulta que devolve um inteiro.
func (e *UsageEnv) Count(t *testing.T, q string, args ...any) int64 {
	t.Helper()
	var n sql.NullInt64
	if err := e.DB.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n.Int64
}

func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		out := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(out, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}
