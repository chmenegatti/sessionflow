package terminal

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// macOS: emuladores que aceitam argv direto, no PATH ou como .app em
// /Applications; o Terminal.app não entra (exigiria AppleScript ou shell).
func TestResolveDarwinFindsAppBundle(t *testing.T) {
	apps := t.TempDir()
	old := macAppDirs
	macAppDirs = []string{apps}
	t.Cleanup(func() { macAppDirs = old })
	bin := filepath.Join(apps, "kitty.app", "Contents", "MacOS", "kitty")
	os.MkdirAll(filepath.Dir(bin), 0o755)
	os.WriteFile(bin, nil, 0o755)
	noLook := func(string) (string, error) { return "", errors.New("não") }
	tm, err := Resolve("darwin", Config{}, noLook)
	if err != nil || tm.Path != bin || !slices.Equal(tm.Args, known["kitty"]) {
		t.Fatalf("%+v %v", tm, err)
	}
}

func TestResolveDarwinPrefersPATH(t *testing.T) {
	wez := exe(t, t.TempDir(), "wezterm", "")
	look := func(n string) (string, error) {
		if n == "wezterm" {
			return wez, nil
		}
		return "", errors.New("não")
	}
	old := macAppDirs
	macAppDirs = []string{t.TempDir()}
	t.Cleanup(func() { macAppDirs = old })
	if tm, err := Resolve("darwin", Config{}, look); err != nil || tm.Path != wez {
		t.Fatalf("%+v %v", tm, err)
	}
}

func TestResolveDarwinNone(t *testing.T) {
	old := macAppDirs
	macAppDirs = []string{t.TempDir()}
	t.Cleanup(func() { macAppDirs = old })
	noLook := func(string) (string, error) { return "", errors.New("não") }
	if _, err := Resolve("darwin", Config{}, noLook); !errors.Is(err, ErrNoTerminal) {
		t.Errorf("sem emulador: %v", err)
	}
}
