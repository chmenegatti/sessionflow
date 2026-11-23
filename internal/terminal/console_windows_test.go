//go:build windows

package terminal

import (
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// Processo filho do teste: grava se a entrada padrão é um console.
func TestConsoleProbe(t *testing.T) {
	out := os.Getenv("SF_CONSOLE_PROBE")
	if out == "" {
		t.Skip("só como filho de TestStartConsoleGivesConsole")
	}
	var mode uint32
	res := "nocon"
	if windows.GetConsoleMode(windows.Handle(os.Stdin.Fd()), &mode) == nil {
		res = "console"
	}
	os.WriteFile(out, []byte(res), 0o600) //nolint:errcheck // o pai confere
}

// A CLI aberta no console de reserva precisa ver o console (TUI), não NUL.
func TestStartConsoleGivesConsole(t *testing.T) {
	dir := t.TempDir()
	out := dir + `\probe.txt`
	t.Setenv("SF_CONSOLE_PROBE", out)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := startConsole([]string{exe, "-test.run=^TestConsoleProbe$"}, dir); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(out); err == nil && len(b) > 0 {
			if got := strings.TrimSpace(string(b)); got != "console" {
				t.Fatalf("entrada padrão do filho: %s", got)
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("filho não respondeu")
}
