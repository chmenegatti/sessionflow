package sqlite

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
	"testing/fstest"
)

var discard = slog.New(slog.NewTextHandler(io.Discard, nil))

func mustExec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

func count(t *testing.T, db *sql.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

func TestOpenIndexCreatesSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "index.db")
	db, rebuilt, err := OpenIndex(context.Background(), path, discard)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if rebuilt {
		t.Error("banco novo não deveria contar como rebuild")
	}
	for _, table := range []string{"projects", "sessions", "messages", "messages_fts", "sync_state"} {
		if count(t, db, "SELECT count(*) FROM sqlite_master WHERE name = ?", table) != 1 {
			t.Errorf("tabela %s ausente", table)
		}
	}
	var mode string
	db.QueryRow("PRAGMA journal_mode").Scan(&mode)
	if mode != "wal" {
		t.Errorf("journal_mode = %s", mode)
	}
	if count(t, db, "PRAGMA foreign_keys") != 1 {
		t.Error("foreign_keys desligado")
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Errorf("permissão %v", info.Mode().Perm())
	}
}

func TestOpenIndexIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "index.db")
	for i := 0; i < 2; i++ {
		db, rebuilt, err := OpenIndex(context.Background(), path, discard)
		if err != nil || rebuilt {
			t.Fatalf("abertura %d: rebuilt=%v err=%v", i, rebuilt, err)
		}
		if v := count(t, db, "PRAGMA user_version"); v != indexVersion(t) {
			t.Errorf("user_version = %d", v)
		}
		db.Close()
	}
}

// insertSession cria projeto + sessão e devolve o id da sessão.
func insertSession(t *testing.T, db *sql.DB, provider, nativeID string) int64 {
	t.Helper()
	mustExec(t, db, "INSERT OR IGNORE INTO projects(path, name) VALUES ('/p/demo', 'demo')")
	res, err := db.Exec(`INSERT INTO sessions(provider, native_id, project_id, source_kind, source_path)
		VALUES (?, ?, (SELECT id FROM projects WHERE path='/p/demo'), 'jsonl', '/x.jsonl')`, provider, nativeID)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

func TestFTSTriggersAndSearch(t *testing.T) {
	db, _, err := OpenIndex(context.Background(), filepath.Join(t.TempDir(), "index.db"), discard)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	sid := insertSession(t, db, "claude", "s1")
	mustExec(t, db, "INSERT INTO messages(session_id, seq, role, search_text) VALUES (?, 1, 'user', 'Migração do repositório concluída')", sid)
	mustExec(t, db, "INSERT INTO messages(session_id, seq, role, search_text) VALUES (?, 2, 'assistant', 'func ReadSession(ctx context.Context)')", sid)

	match := func(q string) int {
		return count(t, db, "SELECT count(*) FROM messages_fts WHERE messages_fts MATCH ?", q)
	}
	if match("migracao") != 1 {
		t.Error("busca sem acento falhou")
	}
	if match(`"repositório concluída"`) != 1 {
		t.Error("busca por frase falhou")
	}
	if match("Read*") != 1 {
		t.Error("busca por prefixo falhou")
	}

	var hl string
	db.QueryRow(`SELECT highlight(messages_fts, 0, '[', ']') FROM messages_fts WHERE messages_fts MATCH 'migracao'`).Scan(&hl)
	if !strings.Contains(hl, "[Migração]") {
		t.Errorf("highlight: %q", hl)
	}

	mustExec(t, db, "UPDATE messages SET search_text = 'texto novo' WHERE seq = 1")
	if match("migracao") != 0 || match("novo") != 1 {
		t.Error("trigger de update não sincronizou o FTS")
	}
	// Apagar a sessão apaga mensagens (cascade) e o FTS (trigger).
	mustExec(t, db, "DELETE FROM sessions WHERE id = ?", sid)
	if match("novo") != 0 || match("Read*") != 0 {
		t.Error("cascade + trigger de delete não limparam o FTS")
	}
	mustExec(t, db, "INSERT INTO messages_fts(messages_fts) VALUES('integrity-check')")
}

func TestSessionIdentityIsProviderScoped(t *testing.T) {
	db, _, err := OpenIndex(context.Background(), filepath.Join(t.TempDir(), "index.db"), discard)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	insertSession(t, db, "claude", "same-id")
	insertSession(t, db, "codex", "same-id")
	if _, err := db.Exec(`INSERT INTO sessions(provider, native_id, source_kind, source_path) VALUES ('claude','same-id','jsonl','/y')`); err == nil {
		t.Fatal("duplicata (provider, native_id) aceita")
	}
}

func TestOpenIndexRecoversFromGarbage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "index.db")
	os.WriteFile(path, []byte(strings.Repeat("isto não é sqlite ", 512)), 0o600)

	db, rebuilt, err := OpenIndex(context.Background(), path, discard)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if !rebuilt {
		t.Fatal("esperava rebuild")
	}
	if count(t, db, "SELECT count(*) FROM sessions") != 0 {
		t.Error("índice recriado deveria estar vazio")
	}
	backups, _ := filepath.Glob(path + ".corrupt-*")
	if len(backups) != 1 {
		t.Fatalf("backup do arquivo corrompido: %v", backups)
	}
}

func TestOpenIndexRecoversFromTruncatedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "index.db")
	db, _, err := OpenIndex(context.Background(), path, discard)
	if err != nil {
		t.Fatal(err)
	}
	sid := insertSession(t, db, "claude", "s1")
	for i := 0; i < 200; i++ {
		mustExec(t, db, "INSERT INTO messages(session_id, seq, role, search_text) VALUES (?, ?, 'user', ?)", sid, i, strings.Repeat("palavra ", 50))
	}
	mustExec(t, db, "PRAGMA wal_checkpoint(TRUNCATE)")
	db.Close()

	info, _ := os.Stat(path)
	if err := os.Truncate(path, info.Size()/2); err != nil {
		t.Fatal(err)
	}
	db, rebuilt, err := OpenIndex(context.Background(), path, discard)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if !rebuilt {
		t.Fatal("arquivo truncado deveria disparar rebuild")
	}
}

func TestOpenIndexRebuildsWhenSchemaTooNew(t *testing.T) {
	path := filepath.Join(t.TempDir(), "index.db")
	db, _, _ := OpenIndex(context.Background(), path, discard)
	mustExec(t, db, "PRAGMA user_version = 999")
	db.Close()

	db, rebuilt, err := OpenIndex(context.Background(), path, discard)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if !rebuilt || count(t, db, "PRAGMA user_version") != indexVersion(t) {
		t.Fatal("esquema futuro deveria ser descartado no índice")
	}
}

func TestOpenUserNeverDiscards(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "user.db")
	db, err := OpenUser(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, "INSERT INTO favorites(provider, native_id, created_at) VALUES ('claude','s1',1)")
	mustExec(t, db, "INSERT INTO tags(name) VALUES ('Go')")
	if _, err := db.Exec("INSERT INTO tags(name) VALUES ('go')"); err == nil {
		t.Error("tags deveriam ser únicas sem diferenciar maiúsculas")
	}
	mustExec(t, db, "PRAGMA user_version = 999")
	db.Close()

	if _, err := OpenUser(context.Background(), path); !errors.Is(err, ErrSchemaTooNew) {
		t.Fatalf("esperava ErrSchemaTooNew, veio %v", err)
	}
	if backups, _ := filepath.Glob(path + ".corrupt-*"); len(backups) != 0 {
		t.Fatal("user.db nunca pode ser movido automaticamente")
	}
}

func TestUserDataSurvivesIndexRebuild(t *testing.T) {
	dir := t.TempDir()
	user, err := OpenUser(context.Background(), filepath.Join(dir, "user.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer user.Close()
	mustExec(t, user, "INSERT INTO favorites(provider, native_id, created_at) VALUES ('codex','abc',1)")

	idx := filepath.Join(dir, "index.db")
	os.WriteFile(idx, []byte("lixo"), 0o600)
	db, rebuilt, err := OpenIndex(context.Background(), idx, discard)
	if err != nil || !rebuilt {
		t.Fatalf("rebuilt=%v err=%v", rebuilt, err)
	}
	db.Close()
	if count(t, user, "SELECT count(*) FROM favorites") != 1 {
		t.Fatal("favorito perdido após rebuild do índice")
	}
}

func TestLoadMigrationsValidation(t *testing.T) {
	ok := fstest.MapFS{
		"m/0002_b.sql": {Data: []byte("CREATE TABLE b(x);")},
		"m/0001_a.sql": {Data: []byte("CREATE TABLE a(x);")},
		"m/README.txt": {Data: []byte("ignorado")},
	}
	migs, err := loadMigrations(ok, "m")
	if err != nil || len(migs) != 2 || migs[0].version != 1 || migs[1].version != 2 {
		t.Fatalf("got %+v, %v", migs, err)
	}
	gap := fstest.MapFS{"m/0001_a.sql": {}, "m/0003_c.sql": {}}
	if _, err := loadMigrations(gap, "m"); err == nil {
		t.Error("lacuna na sequência aceita")
	}
	bad := fstest.MapFS{"m/abc.sql": {}}
	if _, err := loadMigrations(bad, "m"); err == nil {
		t.Error("nome inválido aceito")
	}
}

func TestMigrationFailureRollsBack(t *testing.T) {
	db, err := open(context.Background(), filepath.Join(t.TempDir(), "x.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	migs := []migration{
		{version: 1, name: "0001_ok.sql", sql: "CREATE TABLE a(x);"},
		{version: 2, name: "0002_bad.sql", sql: "CREATE TABLE b(x); ISTO NÃO É SQL;"},
	}
	if err := migrate(context.Background(), db, migs); err == nil {
		t.Fatal("esperava erro")
	}
	if v := count(t, db, "PRAGMA user_version"); v != 1 {
		t.Errorf("user_version = %d, quer 1", v)
	}
	if count(t, db, "SELECT count(*) FROM sqlite_master WHERE name='b'") != 0 {
		t.Error("migration com erro deixou tabela parcial")
	}
}

// Caracteres com significado em URI ("?", "#", "%") no caminho não podem
// desviar o banco para outro arquivo.
func TestOpenHandlesURICharsInPath(t *testing.T) {
	for _, dir := range []string{"a?b", "a#b", "a%20b", "a b"} {
		t.Run(dir, func(t *testing.T) {
			base := t.TempDir()
			path := filepath.Join(base, dir, "index.db")
			db, _, err := OpenIndex(context.Background(), path, discard)
			if err != nil {
				t.Fatal(err)
			}
			db.Close()
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("banco não criado no caminho pedido: %v", err)
			}
			entries, _ := os.ReadDir(base)
			if len(entries) != 1 || entries[0].Name() != dir {
				t.Fatalf("arquivos inesperados em %s: %v", base, entries)
			}
		})
	}
}

// Erro de acesso não é corrupção: o índice não pode ser movido.
func TestOpenIndexDoesNotDiscardOnPermissionError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root ignora permissões de arquivo")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "index.db")
	db, _, err := OpenIndex(context.Background(), path, discard)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(path, 0o600) })

	_, rebuilt, err := OpenIndex(context.Background(), path, discard)
	if err == nil || errors.Is(err, ErrCorrupt) || rebuilt {
		t.Fatalf("esperava erro não-corrupção sem rebuild; err=%v rebuilt=%v", err, rebuilt)
	}
	matches, _ := filepath.Glob(path + ".corrupt-*")
	if len(matches) != 0 {
		t.Fatalf("índice movido indevidamente: %v", matches)
	}
}

func indexVersion(t *testing.T) int {
	t.Helper()
	migs, err := loadMigrations(migrationsFS, "migrations/index")
	if err != nil {
		t.Fatal(err)
	}
	return len(migs)
}

// 0002: índice vindo da v1 perde o sync_state (força releitura) e mantém
// as sessões até a releitura substituí-las.
func TestMigration0002ResetsSyncState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "index.db")
	migs, _ := loadMigrations(migrationsFS, "migrations/index")
	db, err := open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := migrate(context.Background(), db, migs[:1]); err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, "INSERT INTO sync_state (provider, source_path, updated_at) VALUES ('claude', '/a', 1)")
	mustExec(t, db, "INSERT INTO sessions (provider, native_id, source_kind, source_path) VALUES ('claude', 'x', 'jsonl', '/a')")
	db.Close()

	db, _, err = OpenIndex(context.Background(), path, discard)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if count(t, db, "SELECT count(*) FROM sync_state") != 0 || count(t, db, "SELECT count(*) FROM sessions") != 1 {
		t.Error("0002 deveria limpar só o sync_state")
	}
}

// 0003: colunas de estatística em messages e releitura forçada (has_text e
// tools só existem depois que o indexador relê as fontes).
func TestMigration0003ForcesReindex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "index.db")
	migs, _ := loadMigrations(migrationsFS, "migrations/index")
	db, err := open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := migrate(context.Background(), db, migs[:2]); err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, "INSERT INTO sync_state (provider, source_path, updated_at) VALUES ('claude', '/a', 1)")
	db.Close()

	db, _, err = OpenIndex(context.Background(), path, discard)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if v := count(t, db, "PRAGMA user_version"); v < 3 {
		t.Fatalf("versão %d", v)
	}
	if count(t, db, "SELECT count(*) FROM sync_state") != 0 {
		t.Error("sync_state não foi limpo")
	}
	if count(t, db, "SELECT count(*) FROM pragma_table_info('messages') WHERE name IN ('has_text', 'tools')") != 2 {
		t.Error("colunas ausentes")
	}
	if count(t, db, "SELECT count(*) FROM pragma_index_list('messages') WHERE name = 'messages_ts_idx'") != 1 {
		t.Error("índice em ts ausente")
	}
}

func TestMigration0004ForcesReindex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "index.db")
	migs, _ := loadMigrations(migrationsFS, "migrations/index")
	db, err := open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := migrate(context.Background(), db, migs[:3]); err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, "INSERT INTO sync_state (provider, source_path, updated_at) VALUES ('claude', '/a', 1)")
	db.Close()

	db, _, err = OpenIndex(context.Background(), path, discard)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if v := count(t, db, "PRAGMA user_version"); v < 4 {
		t.Fatalf("versão %d", v)
	}
	if count(t, db, "SELECT count(*) FROM sync_state") != 0 {
		t.Error("sync_state não foi limpo")
	}
	cols := "'input_tokens','output_tokens','cache_read_tokens','cache_creation_tokens','reasoning_tokens','total_tokens'"
	if count(t, db, "SELECT count(*) FROM pragma_table_info('usage') WHERE name IN ("+cols+") AND \"notnull\" = 0") != 6 {
		t.Error("colunas de tokens ausentes ou NOT NULL")
	}
}
