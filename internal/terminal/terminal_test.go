package terminal

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

// Argumentos que um shell interpretaria: precisam chegar intactos.
var tricky = []string{"--resume", "id; rm -rf ~", "$(whoami)", "a b", `"q"`, "*"}

func exe(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if runtime.GOOS == "windows" && !strings.HasSuffix(name, ".exe") {
		p += ".exe" // só .exe é executável no Windows
	}
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestArgvPerTerminal(t *testing.T) {
	cmd := Command{Path: "/bin/agent", Args: tricky, Dir: "/proj dir"}
	full := append([]string{"/bin/agent"}, tricky...)
	for name, want := range map[string][]string{
		"ptyxis":         append([]string{"--new-window", "--working-directory", "/proj dir", "--"}, full...),
		"gnome-terminal": append([]string{"--window", "--working-directory", "/proj dir", "--"}, full...),
		"konsole":        append([]string{"--workdir", "/proj dir", "-e"}, full...),
		"kitty":          append([]string{"--directory", "/proj dir"}, full...),
		"ghostty":        append([]string{"--working-directory=/proj dir", "-e"}, full...),
		"xterm":          append([]string{"-e"}, full...),
	} {
		got := Terminal{Path: "/usr/bin/" + name, Args: known[name]}.Argv(cmd)
		if !slices.Equal(got[1:], want) || got[0] != "/usr/bin/"+name {
			t.Errorf("%s:\n got %q\nwant %q", name, got[1:], want)
		}
	}
	for name, args := range known {
		if err := validTemplate(args); err != nil {
			t.Errorf("%s: modelo inválido: %v", name, err)
		}
	}
	for _, name := range autoDetect {
		if !Known(name) {
			t.Errorf("%s na detecção sem sintaxe conhecida", name)
		}
	}
}

func TestResolve(t *testing.T) {
	skipExecBits(t)
	dir := t.TempDir()
	kitty := exe(t, dir, "kitty", "")
	custom := exe(t, dir, "meu-term", "")
	notExec := filepath.Join(dir, "plain")
	os.WriteFile(notExec, nil, 0o644)
	noLook := func(string) (string, error) { return "", errors.New("não") }

	if tm, err := Resolve("linux", Config{Path: kitty}, noLook); err != nil || !slices.Equal(tm.Args, known["kitty"]) {
		t.Errorf("conhecido: %+v %v", tm, err)
	}
	if tm, err := Resolve("linux", Config{Path: custom}, noLook); err != nil || !slices.Equal(tm.Args, []string{"-e", PlaceholderCmd}) {
		t.Errorf("desconhecido: %+v %v", tm, err)
	}
	if tm, err := Resolve("linux", Config{Path: custom, Args: []string{"--cwd={dir}", "run", "{cmd}"}}, noLook); err != nil ||
		!slices.Equal(tm.Argv(Command{Path: "/a", Args: []string{"x"}, Dir: "/d"}), []string{custom, "--cwd=/d", "run", "/a", "x"}) {
		t.Errorf("personalizado: %+v %v", tm, err)
	}
	for _, args := range [][]string{{"-e"}, {"{cmd}", "{cmd}"}, {"-e", "sh -c {cmd}"}} {
		if _, err := Resolve("linux", Config{Path: custom, Args: args}, noLook); !errors.Is(err, ErrInvalidTemplate) {
			t.Errorf("%q: %v", args, err)
		}
	}
	for _, p := range []string{notExec, "kitty", filepath.Join(dir, "nao-existe")} {
		if _, err := Resolve("linux", Config{Path: p}, noLook); !errors.Is(err, ErrInvalidTerminal) {
			t.Errorf("%s: %v", p, err)
		}
	}
	// Detecção: primeiro conhecido no PATH, na ordem de autoDetect.
	ptyxis := exe(t, dir, "ptyxis", "")
	look := func(name string) (string, error) {
		switch name {
		case "ptyxis":
			return ptyxis, nil
		case "kitty":
			return kitty, nil
		}
		return "", errors.New("não")
	}
	if tm, err := Resolve("linux", Config{}, look); err != nil || tm.Path != ptyxis || tm.Name() != "ptyxis" {
		t.Errorf("detecção: %+v %v", tm, err)
	}
	if _, err := Resolve("linux", Config{}, noLook); !errors.Is(err, ErrNoTerminal) {
		t.Errorf("nenhum: %v", err)
	}
}

// O launcher real inicia o "terminal" sem shell, no diretório pedido, e os
// argumentos chegam um a um, sem expansão.
func TestExecLauncherPassesArgvVerbatim(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("o terminal falso é um script sh; no Windows o argv é coberto por TestResolveWindows*")
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "argv.txt")
	// O terminal falso é um script de teste; o app não usa shell.
	term := exe(t, dir, "fake-term", "#!/bin/sh\npwd > \"$OUT.tmp\"\nfor a in \"$@\"; do printf '%s\\n' \"$a\" >> \"$OUT.tmp\"; done\nmv \"$OUT.tmp\" \"$OUT\"\n")
	agent := exe(t, dir, "agent", "")
	work := filepath.Join(dir, "projeto com espaço")
	os.Mkdir(work, 0o755)
	t.Setenv("OUT", out)

	tm, err := Resolve("linux", Config{Path: term, Args: []string{"--wd", "{dir}", "--", "{cmd}"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := (ExecLauncher{}).Launch(context.Background(), tm, Command{Path: agent, Args: tricky, Dir: work}); err != nil {
		t.Fatal(err)
	}
	var data []byte
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if data, err = os.ReadFile(out); err == nil {
			break
		}
	}
	got := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if real, err := filepath.EvalSymlinks(work); err == nil && got[0] == real {
		got[0] = work // macOS: /var é link para /private/var e o pwd mostra o real
	}
	want := append([]string{work, "--wd", work, "--", agent}, tricky...)
	if !slices.Equal(got, want) {
		t.Errorf("argv:\n got %q\nwant %q", got, want)
	}
}

func TestExecLauncherValidates(t *testing.T) {
	dir := t.TempDir()
	agent := exe(t, dir, "agent", "")
	tm := Terminal{Path: exe(t, dir, "t", ""), Args: []string{"{cmd}"}}
	for name, c := range map[string]Command{
		"dir relativo":       {Path: agent, Dir: "rel"},
		"dir ausente":        {Path: agent, Dir: filepath.Join(dir, "x")},
		"dir é arquivo":      {Path: agent, Dir: agent},
		"exe relativo":       {Path: "agent", Dir: dir},
		"exe não executável": {Path: filepath.Join(dir, "nada"), Dir: dir},
		"argumento vazio":    {Path: agent, Args: []string{""}, Dir: dir},
	} {
		if err := (ExecLauncher{}).Launch(context.Background(), tm, c); err == nil {
			t.Errorf("%s: esperava erro", name)
		}
	}
}

// ptyxis expande $VAR: argumentos ou diretório com "$" são recusados nele
// e aceitos nos demais.
func TestCheckDollar(t *testing.T) {
	pt := Terminal{Path: "/usr/bin/ptyxis", Args: known["ptyxis"]}
	gt := Terminal{Path: "/usr/bin/gnome-terminal", Args: known["gnome-terminal"]}
	ok := Command{Path: "/bin/opencode", Args: []string{"/p/app", "--session", "ses_x"}, Dir: "/p/app"}
	for _, c := range []Command{
		{Path: ok.Path, Args: []string{"/p/$HOME"}, Dir: "/p"},
		{Path: ok.Path, Args: ok.Args, Dir: "/p/$x"},
		{Path: "/opt/$bin/x", Dir: "/p"},
	} {
		if err := pt.Check(c); !errors.Is(err, ErrDollarMangled) {
			t.Errorf("ptyxis aceitou %+v", c)
		}
		if err := gt.Check(c); err != nil {
			t.Errorf("gnome-terminal recusou %+v", c)
		}
	}
	if err := pt.Check(ok); err != nil {
		t.Errorf("comum recusado: %v", err)
	}
	dir := t.TempDir()
	if err := (ExecLauncher{}).Launch(context.Background(), Terminal{Path: exe(t, dir, "ptyxis", ""), Args: known["ptyxis"]},
		Command{Path: exe(t, dir, "a", ""), Args: []string{"$x"}, Dir: dir}); !errors.Is(err, ErrDollarMangled) {
		t.Errorf("launcher: %v", err)
	}
}

// skipExecBits: a detecção do Linux e do macOS exige o bit de execução,
// que não existe no Windows.
func skipExecBits(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bit de execução não existe no Windows; testado no Linux e no macOS")
	}
}
