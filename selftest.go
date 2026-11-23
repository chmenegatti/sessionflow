package main

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"

	"sessionflow/internal/config"
	"sessionflow/internal/domain"
	"sessionflow/internal/indexer"
	"sessionflow/internal/provider"
	"sessionflow/internal/provider/claude"
	"sessionflow/internal/provider/codex"
	"sessionflow/internal/provider/opencode"
	"sessionflow/internal/repository/sqlite"
)

// selftestFixtures: históricos anonimizados dos três agentes
// (docs/discovery/fixtures/usage), embutidos no binário.
//
//go:embed docs/discovery/fixtures/usage
var embeddedFixtures embed.FS

var selftestFixtures, _ = fs.Sub(embeddedFixtures, "docs/discovery/fixtures/usage")

// selftest verifica o app instalado sem abrir a janela: num diretório
// temporário (nunca no home real), cria os bancos com as migrations,
// indexa os fixtures com os adaptadores reais e confere uma sessão de
// cada agente. Imprime "SELFTEST OK" e devolve 0, ou a etapa que falhou e 1.
func selftest(w io.Writer, fixtures fs.FS) int {
	step := "diretório temporário"
	fail := func(err error) int {
		fmt.Fprintf(w, "SELFTEST FALHOU (%s): %v\n", step, err)
		return 1
	}
	tmp, err := os.MkdirTemp("", "sessionflow-selftest-*")
	if err != nil {
		return fail(err)
	}
	defer os.RemoveAll(tmp)
	home := filepath.Join(tmp, "home")

	step = "fixtures"
	roots := map[domain.ProviderID]string{
		domain.ProviderClaude:   filepath.Join(tmp, "claude"),
		domain.ProviderCodex:    filepath.Join(tmp, "codex"),
		domain.ProviderOpenCode: filepath.Join(tmp, "opencode"),
	}
	if err := copyFS(fixtures, "claude", filepath.Join(roots[domain.ProviderClaude], "projects")); err != nil {
		return fail(err)
	}
	if err := copyFS(fixtures, "codex", filepath.Join(roots[domain.ProviderCodex], "sessions")); err != nil {
		return fail(err)
	}
	if err := openCodeDB(fixtures, roots[domain.ProviderOpenCode]); err != nil {
		return fail(err)
	}

	step = "bancos"
	paths, err := config.ResolvePaths(runtime.GOOS, func(string) string { return "" }, home)
	if err != nil {
		return fail(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := context.Background()
	idx, _, err := sqlite.OpenIndex(ctx, paths.IndexDB(), log)
	if err != nil {
		return fail(err)
	}
	defer idx.Close()
	usr, err := sqlite.OpenUser(ctx, paths.UserDB())
	if err != nil {
		return fail(err)
	}
	defer usr.Close()

	step = "indexação"
	providers := []provider.Provider{
		claude.New(claude.Options{RootDir: roots[domain.ProviderClaude], Home: home}),
		codex.New(codex.Options{RootDir: roots[domain.ProviderCodex], Home: home}),
		opencode.New(opencode.Options{RootDir: roots[domain.ProviderOpenCode], Home: home, SnapshotDir: paths.CacheDir}),
	}
	rep := indexer.New(sqlite.NewIndexStore(idx), log, providers...).Run(ctx, indexer.RunOptions{})
	for _, p := range rep.Providers {
		if p.Sessions == 0 || p.Failed > 0 {
			return fail(fmt.Errorf("%s: %d sessões, %d fontes com erro", p.Detection.Provider, p.Sessions, p.Failed))
		}
		fmt.Fprintf(w, "%-9s sessões: %d\n", p.Detection.Provider, p.Sessions)
	}
	if len(rep.Providers) != len(roots) {
		return fail(fmt.Errorf("%d agentes indexados de %d", len(rep.Providers), len(roots)))
	}
	fmt.Fprintln(w, "SELFTEST OK")
	return 0
}

// copyFS copia a pasta dir de src para dst.
func copyFS(src fs.FS, dir, dst string) error {
	return fs.WalkDir(src, dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, filepath.FromSlash(p))
		out := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(out, 0o700)
		}
		data, err := fs.ReadFile(src, p)
		if err != nil {
			return err
		}
		return os.WriteFile(out, data, 0o600)
	})
}

// openCodeDB cria opencode.db em dir a partir do SQL do fixture.
func openCodeDB(src fs.FS, dir string) error {
	q, err := fs.ReadFile(src, "opencode/usage.sql")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "opencode.db"))
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec(string(q))
	return err
}
