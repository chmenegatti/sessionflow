package sqliteread

import (
	"context"
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"

	_ "modernc.org/sqlite"
)

func listDir(t *testing.T, dir string) []string {
	t.Helper()
	es, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range es {
		out = append(out, e.Name())
	}
	return out
}

// writer cria um banco WAL e o mantém aberto (como o OpenCode rodando):
// .db, -wal e -shm existem.
func writer(t *testing.T, dir string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "app.db")+"?_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	for _, q := range []string{
		"CREATE TABLE t(x)",
		"INSERT INTO t VALUES (1), (2), (3)",
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func count(t *testing.T, db *DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT count(*) FROM t").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestOpenReadOnlyWhenShmExists(t *testing.T) {
	dir := t.TempDir()
	writer(t, dir)
	before := listDir(t, dir)
	if !slices.Contains(before, "app.db-shm") {
		t.Fatalf("pré-condição: %v", before)
	}

	db, err := Open(context.Background(), filepath.Join(dir, "app.db"), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if db.Snapshot || count(t, db) != 3 {
		t.Fatalf("snapshot=%v", db.Snapshot)
	}
	if _, err := db.Exec("INSERT INTO t VALUES (4)"); err == nil {
		t.Error("escrita deveria falhar")
	}
	db.Close()
	if after := listDir(t, dir); !slices.Equal(before, after) {
		t.Errorf("diretório do provider mudou: %v → %v", before, after)
	}
}

func copyTo(t *testing.T, src, dst string) {
	t.Helper()
	in, err := os.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	io.Copy(out, in)
}

// Sem -shm, ler o original o criaria. A leitura tem de ir para uma cópia,
// mesmo com o diretório do provider somente leitura.
func TestOpenSnapshotWhenShmMissing(t *testing.T) {
	live := t.TempDir()
	writer(t, live)
	dir := t.TempDir()
	copyTo(t, filepath.Join(live, "app.db"), filepath.Join(dir, "app.db"))
	copyTo(t, filepath.Join(live, "app.db-wal"), filepath.Join(dir, "app.db-wal"))
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
	before := listDir(t, dir)

	tmp := t.TempDir()
	db, err := Open(context.Background(), filepath.Join(dir, "app.db"), tmp)
	if err != nil {
		t.Fatal(err)
	}
	if !db.Snapshot || count(t, db) != 3 {
		t.Fatalf("snapshot=%v", db.Snapshot)
	}
	db.Close()
	if after := listDir(t, dir); !slices.Equal(before, after) {
		t.Errorf("diretório do provider mudou: %v → %v", before, after)
	}
	if left := listDir(t, tmp); len(left) != 0 {
		t.Errorf("cópia não foi apagada: %v", left)
	}
}

func TestOpenMissingAndGarbage(t *testing.T) {
	dir := t.TempDir()
	if _, err := Open(context.Background(), filepath.Join(dir, "nada.db"), t.TempDir()); err == nil {
		t.Error("arquivo ausente deveria falhar")
	}
	bad := filepath.Join(dir, "lixo.db")
	os.WriteFile(bad, []byte("isto não é sqlite, só lixo com tamanho suficiente para o header"), 0o644)
	tmp := t.TempDir()
	if _, err := Open(context.Background(), bad, tmp); err == nil {
		t.Error("lixo deveria falhar")
	}
	if left := listDir(t, tmp); len(left) != 0 {
		t.Errorf("cópias deixadas: %v", left)
	}
}

func TestCleanStaleAndColumns(t *testing.T) {
	tmp := t.TempDir()
	os.MkdirAll(filepath.Join(tmp, "snap-123"), 0o700)
	os.WriteFile(filepath.Join(tmp, "outro"), nil, 0o600)
	if err := CleanStale(tmp); err != nil {
		t.Fatal(err)
	}
	if got := listDir(t, tmp); !slices.Equal(got, []string{"outro"}) {
		t.Errorf("restou %v", got)
	}

	dir := t.TempDir()
	writer(t, dir)
	db, err := Open(context.Background(), filepath.Join(dir, "app.db"), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tables, _ := Tables(context.Background(), db.DB)
	cols, _ := Columns(context.Background(), db.DB, "t")
	if !tables["t"] || !cols["x"] || len(cols) != 1 {
		t.Errorf("tables=%v cols=%v", tables, cols)
	}
}
