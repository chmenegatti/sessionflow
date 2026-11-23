package terminal

import (
	"errors"
	"slices"
	"testing"
)

// Retomada no Windows (design 0.2.0): Windows Terminal se existir, senão
// uma console nova; nunca cmd /c.

var winCmd = Command{Path: `C:\Users\José Silva\.local\bin\claude.exe`, Args: []string{"--resume", "123e4567-e89b-12d3-a456-426614174000"}, Dir: `C:\Users\José Silva\proj`}

func TestResolveWindowsPrefersWT(t *testing.T) {
	wt := exe(t, t.TempDir(), "wt.exe", "")
	look := func(name string) (string, error) {
		if name == "wt" {
			return wt, nil
		}
		return "", errors.New("não")
	}
	tm, err := Resolve("windows", Config{}, look)
	if err != nil || tm.Console {
		t.Fatalf("%+v %v", tm, err)
	}
	want := []string{wt, "-d", winCmd.Dir, "--", winCmd.Path, "--resume", winCmd.Args[1]}
	if got := tm.Argv(winCmd); !slices.Equal(got, want) {
		t.Errorf("argv\n got %q\nwant %q", got, want)
	}
}

func TestResolveWindowsConsoleFallback(t *testing.T) {
	noLook := func(string) (string, error) { return "", errors.New("não") }
	tm, err := Resolve("windows", Config{}, noLook)
	if err != nil || !tm.Console || tm.Name() != "console" {
		t.Fatalf("%+v %v", tm, err)
	}
	if got := tm.Argv(winCmd); !slices.Equal(got, winCmd.Argv()) {
		t.Errorf("console deve rodar o comando direto: %q", got)
	}
}

// O wt separa comandos por ";": diretório ou argumento com ";" abriria
// outra aba com o resto como comando.
func TestCheckSemicolonWT(t *testing.T) {
	tm := Terminal{Path: `C:\Windows\wt.exe`, Args: known["wt"]}
	bad := winCmd
	bad.Dir = `C:\proj;calc`
	if err := tm.Check(bad); !errors.Is(err, ErrSemicolon) {
		t.Errorf("diretório com ';': %v", err)
	}
	if err := tm.Check(winCmd); err != nil {
		t.Errorf("comando normal: %v", err)
	}
}
