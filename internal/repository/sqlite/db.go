// Package sqlite abre e migra os bancos próprios do SessionFlow:
//
//   - index.db: índice reconstruível a partir dos históricos (sessões,
//     mensagens normalizadas, FTS5, estado de sincronização). Se estiver
//     corrompido ou com esquema de versão futura, é renomeado e recriado.
//   - user.db: dados do usuário que não podem ser reconstruídos (favoritos,
//     tags). Nunca é descartado automaticamente.
//
// Driver: modernc.org/sqlite (puro Go, FTS5 embutido). Ver ADR 0002.
package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"time"

	msqlite "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

//go:embed migrations
var migrationsFS embed.FS

// ErrCorrupt indica banco ilegível ou com falha de integridade.
var ErrCorrupt = errors.New("banco corrompido")

// classify marca como ErrCorrupt apenas os erros que o SQLite reporta como
// corrupção (SQLITE_CORRUPT, SQLITE_NOTADB). Permissão negada, disco cheio
// ou banco ocupado não são corrupção: descartar o índice nesses casos só
// geraria cópias e reindexação sem resolver a causa.
func classify(op string, err error) error {
	var se *msqlite.Error
	if errors.As(err, &se) {
		switch se.Code() & 0xff { // código primário; o estendido traz o detalhe
		case sqlite3.SQLITE_CORRUPT, sqlite3.SQLITE_NOTADB:
			return fmt.Errorf("%w: %s: %v", ErrCorrupt, op, err)
		}
	}
	return fmt.Errorf("%s: %w", op, err)
}

// dsn monta a URI do SQLite. O caminho é escapado: "?", "#" e "%" num
// diretório (ex.: XDG_DATA_HOME) seriam interpretados como query, fragmento
// ou escape e abririam outro arquivo.
func dsn(path string) string {
	q := url.Values{}
	for _, p := range []string{
		"journal_mode(WAL)",
		"foreign_keys(1)",
		"busy_timeout(5000)",
		"synchronous(NORMAL)",
	} {
		q.Add("_pragma", p)
	}
	// BEGIN IMMEDIATE: a transação pega o lock de escrita no início. Com
	// BEGIN DEFERRED, promover leitura a escrita falha com SQLITE_BUSY na
	// hora, sem respeitar o busy_timeout.
	q.Set("_txlock", "immediate")
	return "file:" + (&url.URL{Path: path}).EscapedPath() + "?" + q.Encode()
}

// open abre um banco local com os PRAGMAs padrão e testa a conexão.
func open(ctx context.Context, path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		return nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, classify("abrir", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// OpenUser abre (criando se preciso) e migra o banco do usuário.
func OpenUser(ctx context.Context, path string) (*sql.DB, error) {
	db, err := open(ctx, path)
	if err != nil {
		return nil, err
	}
	migs, err := loadMigrations(migrationsFS, "migrations/user")
	if err == nil {
		err = migrate(ctx, db, migs)
	}
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("user.db: %w", err)
	}
	return db, nil
}

// OpenIndex abre e migra o índice. Corrupção, falha de integridade do FTS ou
// esquema mais novo que o binário fazem o arquivo ser movido para
// "<path>.corrupt-<timestamp>" e um índice vazio ser criado — o próximo scan
// o repopula a partir dos históricos originais.
// rebuilt informa se isso aconteceu.
func OpenIndex(ctx context.Context, path string, log *slog.Logger) (db *sql.DB, rebuilt bool, err error) {
	migs, err := loadMigrations(migrationsFS, "migrations/index")
	if err != nil {
		return nil, false, err
	}
	db, err = openIndexOnce(ctx, path, migs)
	if err == nil {
		return db, false, nil
	}
	if !errors.Is(err, ErrCorrupt) && !errors.Is(err, ErrSchemaTooNew) {
		return nil, false, err
	}
	backup := fmt.Sprintf("%s.corrupt-%d", path, time.Now().UnixNano())
	log.Warn("índice inválido; recriando", "path", path, "backup", backup, "err", err)
	if err := moveDBFiles(path, backup); err != nil {
		return nil, false, err
	}
	db, err = openIndexOnce(ctx, path, migs)
	return db, err == nil, err
}

func openIndexOnce(ctx context.Context, path string, migs []migration) (*sql.DB, error) {
	db, err := open(ctx, path)
	if err != nil {
		return nil, err
	}
	if err = checkIntegrity(ctx, db); err == nil {
		if err = migrate(ctx, db, migs); err == nil {
			err = checkFTS(ctx, db)
		}
	}
	if err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func checkIntegrity(ctx context.Context, db *sql.DB) error {
	var res string
	if err := db.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&res); err != nil {
		return classify("quick_check", err)
	}
	if res != "ok" {
		return fmt.Errorf("%w: quick_check: %s", ErrCorrupt, res)
	}
	return nil
}

func checkFTS(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, "INSERT INTO messages_fts(messages_fts) VALUES('integrity-check')"); err != nil {
		return classify("fts", err)
	}
	return nil
}

// moveDBFiles renomeia o banco e seus arquivos -wal/-shm.
func moveDBFiles(path, dest string) error {
	for _, suffix := range []string{"", "-wal", "-shm"} {
		err := os.Rename(path+suffix, dest+suffix)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}
