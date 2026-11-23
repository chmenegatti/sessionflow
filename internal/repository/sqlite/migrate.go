package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"
)

// ErrSchemaTooNew indica banco migrado por uma versão mais nova do app.
var ErrSchemaTooNew = errors.New("esquema mais novo que esta versão do SessionFlow")

type migration struct {
	version int
	name    string
	sql     string
}

// loadMigrations lê arquivos "NNNN_descricao.sql" de dir. As versões devem
// ser 1, 2, 3… sem lacunas.
func loadMigrations(fsys fs.FS, dir string) ([]migration, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, err
	}
	var migs []migration
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		num, _, ok := strings.Cut(e.Name(), "_")
		v, err := strconv.Atoi(num)
		if !ok || err != nil || v <= 0 {
			return nil, fmt.Errorf("migration com nome inválido: %s", e.Name())
		}
		body, err := fs.ReadFile(fsys, path.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		migs = append(migs, migration{version: v, name: e.Name(), sql: string(body)})
	}
	sort.Slice(migs, func(i, j int) bool { return migs[i].version < migs[j].version })
	for i, m := range migs {
		if m.version != i+1 {
			return nil, fmt.Errorf("sequência de migrations quebrada em %s", m.name)
		}
	}
	return migs, nil
}

// migrate aplica as migrations pendentes, cada uma em sua transação,
// usando PRAGMA user_version como versão do esquema.
func migrate(ctx context.Context, db *sql.DB, migs []migration) error {
	current, err := userVersion(ctx, db)
	if err != nil {
		return err
	}
	if current > len(migs) {
		return fmt.Errorf("%w: banco v%d, binário v%d", ErrSchemaTooNew, current, len(migs))
	}
	for _, m := range migs[current:] {
		if err := apply(ctx, db, m); err != nil {
			return fmt.Errorf("migration %s: %w", m.name, err)
		}
	}
	return nil
}

func apply(ctx context.Context, db *sql.DB, m migration) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, m.sql); err != nil {
		return err
	}
	// PRAGMA não aceita parâmetro; versão vem de inteiro validado.
	if _, err := tx.ExecContext(ctx, "PRAGMA user_version = "+strconv.Itoa(m.version)); err != nil {
		return err
	}
	return tx.Commit()
}

func userVersion(ctx context.Context, db *sql.DB) (int, error) {
	var v int
	err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&v)
	if err != nil {
		return 0, classify("user_version", err)
	}
	return v, nil
}
