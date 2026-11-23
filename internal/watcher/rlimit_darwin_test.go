//go:build darwin

package watcher

import (
	"syscall"
	"testing"
)

// No macOS o fsnotify usa kqueue: um descritor por pasta observada. O
// runtime do Go (1.19+) já sobe o limite flexível de arquivos abertos ao
// máximo permitido ao iniciar; este teste garante que o processo tem folga
// para observar milhares de pastas antes de cair no modo degradado.
func TestFileLimitRaisedOnDarwin(t *testing.T) {
	var lim syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &lim); err != nil {
		t.Fatal(err)
	}
	if lim.Cur < 4096 {
		t.Errorf("limite flexível de arquivos %d; o watcher degradaria cedo", lim.Cur)
	}
}
