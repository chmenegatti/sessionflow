//go:build unix

package lockfile

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestSecondAcquireFailsUntilRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "sessionflow.lock")
	a, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Acquire(path); !errors.Is(err, ErrLocked) {
		t.Fatalf("segunda instância: %v", err)
	}
	if err := a.Release(); err != nil {
		t.Fatal(err)
	}
	b, err := Acquire(path)
	if err != nil {
		t.Fatalf("após liberar: %v", err)
	}
	b.Release()
}
