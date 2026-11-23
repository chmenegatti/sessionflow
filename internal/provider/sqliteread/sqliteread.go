// Package sqliteread abre bancos SQLite de OUTROS programas (OpenCode) só
// para leitura, sem criar nem alterar arquivos no diretório deles.
//
// Experimento da Fase 0 (docs/discovery/05-sqlite-fts5.md §4): abrir um
// banco WAL com mode=ro cria o arquivo -shm quando ele não existe. Então:
//   - com -shm presente (o dono está rodando ou fechou há pouco): abre o
//     original com mode=ro e query_only;
//   - sem -shm: copia .db e -wal para um diretório temporário privado
//     (0700) e lê a cópia, que é apagada no Close.
package sqliteread

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// DB é um banco aberto para leitura. Close libera também a cópia, se houver.
type DB struct {
	*sql.DB
	snapshotDir string
	// Snapshot informa se a leitura é sobre uma cópia.
	Snapshot bool
}

// Close fecha o banco e apaga a cópia temporária.
func (d *DB) Close() error {
	err := d.DB.Close()
	if d.snapshotDir != "" {
		err = errors.Join(err, os.RemoveAll(d.snapshotDir))
	}
	return err
}

// Open abre path para leitura. tmpDir recebe as cópias (deve ser privado
// ao usuário; ex.: $XDG_CACHE_HOME/sessionflow/snapshots).
func Open(ctx context.Context, path, tmpDir string) (*DB, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	if exists(path + "-shm") {
		db, err := openDSN(ctx, dsn(path, "mode=ro"))
		if err == nil {
			return &DB{DB: db}, nil
		}
		// -shm pode ter sumido entre o Stat e a abertura; cai na cópia.
	}
	return openSnapshot(ctx, path, tmpDir)
}

func openSnapshot(ctx context.Context, path, tmpDir string) (*DB, error) {
	if err := os.MkdirAll(tmpDir, 0o700); err != nil {
		return nil, err
	}
	var lastErr error
	// Uma cópia feita durante uma escrita pode sair inconsistente; tenta
	// de novo uma vez.
	for range 2 {
		dir, err := os.MkdirTemp(tmpDir, "snap-*")
		if err != nil {
			return nil, err
		}
		dst := filepath.Join(dir, "copy.db")
		if err := copyFile(path, dst); err != nil {
			os.RemoveAll(dir)
			return nil, err
		}
		if exists(path + "-wal") {
			if err := copyFile(path+"-wal", dst+"-wal"); err != nil {
				os.RemoveAll(dir)
				return nil, err
			}
		}
		db, err := openDSN(ctx, dsn(dst, ""))
		if err == nil {
			err = quickCheck(ctx, db)
			if err == nil {
				return &DB{DB: db, snapshotDir: dir, Snapshot: true}, nil
			}
			db.Close()
		}
		os.RemoveAll(dir)
		lastErr = err
	}
	return nil, lastErr
}

func dsn(path, extra string) string {
	q := "_pragma=busy_timeout(5000)&_pragma=query_only(1)"
	if extra != "" {
		q = extra + "&" + q
	}
	return "file:" + (&url.URL{Path: path}).EscapedPath() + "?" + q
}

func openDSN(ctx context.Context, dsn string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func quickCheck(ctx context.Context, db *sql.DB) error {
	var res string
	if err := db.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&res); err != nil {
		return err
	}
	if res != "ok" {
		return fmt.Errorf("cópia inconsistente: %s", res)
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// Tables devolve o nome das tabelas do banco.
func Tables(ctx context.Context, db *sql.DB) (map[string]bool, error) {
	return names(ctx, db, "SELECT name FROM sqlite_master WHERE type = 'table'")
}

// Columns devolve as colunas de uma tabela. table deve vir de Tables.
func Columns(ctx context.Context, db *sql.DB, table string) (map[string]bool, error) {
	return names(ctx, db, "SELECT name FROM pragma_table_info(?)", table)
}

func names(ctx context.Context, db *sql.DB, q string, args ...any) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out[n] = true
	}
	return out, rows.Err()
}

// CleanStale apaga cópias deixadas por uma execução interrompida. Só deve
// ser chamado com a instância única garantida.
func CleanStale(tmpDir string) error {
	matches, err := filepath.Glob(filepath.Join(tmpDir, "snap-*"))
	if err != nil {
		return err
	}
	var errs []error
	for _, m := range matches {
		errs = append(errs, os.RemoveAll(m))
	}
	return errors.Join(errs...)
}
