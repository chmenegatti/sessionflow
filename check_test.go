package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// --check num home vazio: não falha, não cria nada e diz o que faltou.
func TestCheckEmptyHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, k := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME", "CLAUDE_CONFIG_DIR", "CODEX_HOME", "OPENCODE_DB"} {
		t.Setenv(k, "")
	}
	t.Setenv("PATH", "")
	var out bytes.Buffer
	if code := check(&out); code != 0 {
		t.Fatalf("código %d\n%s", code, out.String())
	}
	s := out.String()
	for _, want := range []string{"SessionFlow ", "ausente (padrão", "ainda não criado", "Claude Code   not_found", "CLI não encontrada", noTerminal} {
		if !strings.Contains(s, want) {
			t.Errorf("sem %q em:\n%s", want, s)
		}
	}
	// Somente leitura: nada foi criado no home.
	entries, _ := os.ReadDir(home)
	if len(entries) != 0 {
		t.Errorf("criou arquivos: %v", entries)
	}
}

func TestTestTerminalArgs(t *testing.T) {
	var out bytes.Buffer
	if code := testTerminal(&out, []string{"/tmp"}); code != 2 {
		t.Errorf("sem comando: %d", code)
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("PATH", "")
	out.Reset()
	if code := testTerminal(&out, []string{filepath.Join(t.TempDir(), "x"), "/bin/true"}); code != 1 || !strings.Contains(out.String(), "ERRO") {
		t.Errorf("sem terminal: %d %s", code, out.String())
	}
}

// noTerminal: sem PATH, o Linux e o macOS não acham terminal; o Windows
// abre uma console nova.
var noTerminal = map[bool]string{true: "Terminal: console", false: "Terminal: —"}[runtime.GOOS == "windows"]
