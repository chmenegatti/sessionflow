//go:build windows

package sqliteread

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// No Windows, um programa pode abrir o banco sem compartilhar leitura (lock
// obrigatório). Abrir deve falhar rápido com erro, sem travar nem entrar
// em pânico: o provider aparece com erro e os outros seguem.
func TestOpenFailsFastWhenFileLockedExclusively(t *testing.T) {
	dir := t.TempDir()
	w := writer(t, dir)
	w.Close()
	path := filepath.Join(dir, "app.db")
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(h)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	db, err := Open(ctx, path, filepath.Join(dir, "snap"))
	if err == nil {
		db.Close()
		t.Fatal("banco travado aberto sem erro")
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("demorou %v para falhar", d)
	}
}
