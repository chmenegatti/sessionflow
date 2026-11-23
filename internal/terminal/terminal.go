// Package terminal abre comandos interativos (a retomada de uma sessão) num
// emulador de terminal. Nada passa por shell: o emulador recebe o
// executável e os argumentos como itens separados do argv, e cada emulador
// conhecido tem a sintaxe dele para "rode este argv neste diretório".
package terminal

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"sessionflow/internal/host"
)

// Command é um processo a abrir no terminal. Path é absoluto; Args vêm
// depois dele no argv; Dir é o diretório de trabalho (absoluto).
type Command struct {
	Path string   `json:"path"`
	Args []string `json:"args"`
	Dir  string   `json:"dir"`
}

// Argv devolve o argv completo do comando (Path + Args).
func (c Command) Argv() []string { return append([]string{c.Path}, c.Args...) }

// Placeholders aceitos em Config.Args de um terminal personalizado.
const (
	PlaceholderDir = "{dir}" // diretório de trabalho (um item do argv)
	PlaceholderCmd = "{cmd}" // executável + argumentos (vários itens do argv)
)

// Config escolhe o emulador. Vazio = detectar. Path absoluto; Args
// opcionais com {dir} e {cmd}: sem Args, um emulador conhecido usa a
// sintaxe dele e um desconhecido recebe "-e {cmd}".
type Config struct {
	Path string   `json:"path,omitempty"`
	Args []string `json:"args,omitempty"`
}

// known: argumentos de cada emulador, por nome do executável. Ordem de
// detecção = ordem de autoDetect. Sintaxes conferidas no --help de cada um
// (ptyxis 48, gnome-terminal 3.56; demais pela documentação, ver
// docs/PROVIDERS.md §Retomada).
var known = map[string][]string{
	"ptyxis":              {"--new-window", "--working-directory", PlaceholderDir, "--", PlaceholderCmd},
	"gnome-terminal":      {"--window", "--working-directory", PlaceholderDir, "--", PlaceholderCmd},
	"kgx":                 {"--working-directory", PlaceholderDir, "--", PlaceholderCmd},
	"konsole":             {"--workdir", PlaceholderDir, "-e", PlaceholderCmd},
	"kitty":               {"--directory", PlaceholderDir, PlaceholderCmd},
	"alacritty":           {"--working-directory", PlaceholderDir, "-e", PlaceholderCmd},
	"wezterm":             {"start", "--cwd", PlaceholderDir, "--", PlaceholderCmd},
	"foot":                {"--working-directory", PlaceholderDir, PlaceholderCmd},
	"ghostty":             {"--working-directory=" + PlaceholderDir, "-e", PlaceholderCmd},
	"xfce4-terminal":      {"--working-directory", PlaceholderDir, "-x", PlaceholderCmd},
	"xterm":               {"-e", PlaceholderCmd},
	"x-terminal-emulator": {"-e", PlaceholderCmd},
	// Windows Terminal: "--" separa as opções do wt da linha de comando.
	"wt": {"-d", PlaceholderDir, "--", PlaceholderCmd},
}

// splitsOnSemicolon: emuladores que tratam ";" como separador de comandos
// (wt abre outra aba com o resto). Com eles, ";" no diretório ou no
// comando é recusado.
var splitsOnSemicolon = map[string]bool{"wt": true}

// expandsVars: emuladores que expandem $VAR nos argumentos do comando
// (medido: ptyxis 48 troca "$HOME" por /home/…, e "$(x)" e "$" somem; nada
// é executado). Com eles, argumento ou diretório com "$" é recusado.
var expandsVars = map[string]bool{"ptyxis": true}

// autoDetect é a ordem de procura quando nada foi configurado: terminais
// do GNOME e KDE primeiro, x-terminal-emulator por último (é um wrapper
// cuja tradução de -e varia por distribuição). tilix fica de fora: o -e
// dele recebe uma única string e a divide, o que exigiria montar e escapar
// uma linha de comando.
var autoDetect = []string{
	"ptyxis", "gnome-terminal", "kgx", "konsole", "kitty", "alacritty", "wezterm",
	"foot", "ghostty", "xfce4-terminal", "xterm", "x-terminal-emulator",
}

// autoDetectFor: ordem de procura em cada sistema. No Windows, sem o
// Windows Terminal, o comando roda numa console nova (Terminal.Console).
func autoDetectFor(goos string) []string {
	switch goos {
	case "windows":
		return []string{"wt"}
	case "darwin":
		return macTerminals
	}
	return autoDetect
}

// macTerminals: no macOS, só emuladores que recebem o comando como argv
// (o Terminal.app e o iTerm2 exigiriam AppleScript com o comando em
// texto). Ghostty por último: no macOS ele não abre janela nova pela CLI
// em todas as versões.
var macTerminals = []string{"kitty", "wezterm", "alacritty", "ghostty"}

// macAppDirs: onde procurar os .app quando o emulador não está no PATH.
var macAppDirs = []string{"/Applications", filepath.Join(os.Getenv("HOME"), "Applications")}

// macAppBundles: nome do .app e do executável dentro dele.
var macAppBundles = map[string]string{"kitty": "kitty.app", "wezterm": "WezTerm.app", "alacritty": "Alacritty.app", "ghostty": "Ghostty.app"}

func macBundled(name string) string {
	for _, dir := range macAppDirs {
		p := filepath.Join(dir, macAppBundles[name], "Contents", "MacOS", name)
		if isExecutableOS("darwin", p) {
			return p
		}
	}
	return ""
}

// Erros exibidos ao usuário.
var (
	ErrNoTerminal      = errors.New("nenhum emulador de terminal encontrado; configure um em Configurações")
	ErrInvalidTerminal = errors.New("terminal configurado não existe ou não é executável")
	ErrInvalidTemplate = errors.New("argumentos do terminal precisam conter {cmd} exatamente uma vez")
	ErrSemicolon       = errors.New("o Windows Terminal trata \";\" como separador de comandos e o caminho ou comando contém \";\"; escolha outro terminal em Configurações")
	ErrDollarMangled   = errors.New("este terminal expande \"$\" nos argumentos e o caminho ou comando contém \"$\"; escolha outro terminal em Configurações")
)

// LookPath encontra um executável no PATH (exec.LookPath em produção).
type LookPath func(string) (string, error)

// Terminal é um emulador resolvido.
type Terminal struct {
	Path    string   `json:"path"`
	Args    []string `json:"args"`    // modelo com {dir}/{cmd}
	Console bool     `json:"console"` // Windows sem emulador: o comando roda numa console nova
}

// Name é o nome do executável (para exibição), sem ".exe".
func (t Terminal) Name() string {
	if t.Console {
		return "console"
	}
	base := t.Path[strings.LastIndexAny(t.Path, `/\`)+1:] // "\" também: caminho do Windows em qualquer SO
	if strings.EqualFold(filepath.Ext(base), ".exe") {
		base = base[:len(base)-4]
	}
	return base
}

// Resolve escolhe o emulador no sistema goos: o configurado (validado) ou
// o primeiro conhecido encontrado no PATH (no Windows, sem nenhum, uma
// console nova).
func Resolve(goos string, c Config, look LookPath) (Terminal, error) {
	if c.Path != "" {
		if !filepath.IsAbs(c.Path) || !isExecutableOS(goos, c.Path) {
			return Terminal{}, ErrInvalidTerminal
		}
		args := c.Args
		if len(args) == 0 {
			args = known[Terminal{Path: c.Path}.Name()]
		}
		if len(args) == 0 {
			args = []string{"-e", PlaceholderCmd}
		}
		if err := validTemplate(args); err != nil {
			return Terminal{}, err
		}
		return Terminal{Path: c.Path, Args: args}, nil
	}
	for _, name := range autoDetectFor(goos) {
		if p, err := look(name); err == nil && filepath.IsAbs(p) && isExecutableOS(goos, p) {
			return Terminal{Path: p, Args: known[name]}, nil
		}
		if goos == "darwin" {
			if p := macBundled(name); p != "" {
				return Terminal{Path: p, Args: known[name]}, nil
			}
		}
	}
	if goos == "windows" {
		return Terminal{Console: true}, nil
	}
	return Terminal{}, ErrNoTerminal
}

// CheckArgs valida argumentos configurados (vazio = usar o padrão).
func CheckArgs(args []string) error {
	if len(args) == 0 {
		return nil
	}
	return validTemplate(args)
}

func validTemplate(args []string) error {
	n := 0
	for _, a := range args {
		if a == PlaceholderCmd {
			n++
		} else if strings.Contains(a, PlaceholderCmd) {
			return ErrInvalidTemplate // {cmd} colado a outro texto viraria uma string só
		}
	}
	if n != 1 {
		return ErrInvalidTemplate
	}
	return nil
}

// Check confere se o emulador entrega cmd sem alterá-lo.
func (t Terminal) Check(cmd Command) error {
	if splitsOnSemicolon[t.Name()] && (strings.Contains(cmd.Dir, ";") || slices.ContainsFunc(cmd.Argv(), func(a string) bool { return strings.Contains(a, ";") })) {
		return ErrSemicolon
	}
	if !expandsVars[t.Name()] {
		return nil
	}
	if strings.Contains(cmd.Dir, "$") || slices.ContainsFunc(cmd.Argv(), func(a string) bool { return strings.Contains(a, "$") }) {
		return ErrDollarMangled
	}
	return nil
}

// Argv monta o argv do emulador para rodar cmd: {cmd} vira o executável e
// os argumentos (um item cada) e {dir}, o diretório.
func (t Terminal) Argv(cmd Command) []string {
	if t.Console {
		return cmd.Argv()
	}
	out := []string{t.Path}
	for _, a := range t.Args {
		switch {
		case a == PlaceholderCmd:
			out = append(out, cmd.Argv()...)
		case strings.Contains(a, PlaceholderDir):
			out = append(out, strings.ReplaceAll(a, PlaceholderDir, cmd.Dir))
		default:
			out = append(out, a)
		}
	}
	return out
}

// Launcher abre um comando num terminal. Injetável para testes.
type Launcher interface {
	Launch(ctx context.Context, t Terminal, cmd Command) error
}

// ExecLauncher inicia o emulador com exec direto (sem shell), no diretório
// do comando, desligado do processo do app: fechar o SessionFlow não fecha
// a sessão retomada.
type ExecLauncher struct{}

// Launch inicia o emulador e não espera por ele (o processo é recolhido em
// segundo plano para não virar zumbi).
func (ExecLauncher) Launch(_ context.Context, t Terminal, cmd Command) error {
	if err := cmd.validate(); err != nil {
		return err
	}
	if err := t.Check(cmd); err != nil {
		return err
	}
	// Sem o ambiente injetado pela AppImage.
	if t.Console {
		// Sem exec.Cmd: ele passaria NUL como entrada e saída padrão e a
		// CLI não veria o console novo.
		if err := startConsole(t.Argv(cmd), cmd.Dir); err != nil {
			return fmt.Errorf("abrir %s: %w", t.Name(), err)
		}
		return nil
	}
	p := host.Current.Command(t.Argv(cmd), cmd.Dir)
	p.Env = childEnv(runtime.GOOS, p.Env, cmd.Path)
	p.Stdin, p.Stdout, p.Stderr = nil, nil, nil
	detach(p)
	if err := p.Start(); err != nil {
		return fmt.Errorf("abrir %s: %w", t.Name(), err)
	}
	go p.Wait() //nolint:errcheck // terminal pode durar mais que a chamada
	return nil
}

// validate confere o que o executor exige. Quem monta o comando (provider
// e ResumeService) já validou; aqui é a última barreira.
func (c Command) validate() error {
	if !filepath.IsAbs(c.Path) || !isExecutable(c.Path) {
		return fmt.Errorf("executável inválido: %s", c.Path)
	}
	if !filepath.IsAbs(c.Dir) || !isDir(c.Dir) {
		return fmt.Errorf("diretório de trabalho inválido: %s", c.Dir)
	}
	if slices.Contains(c.Args, "") {
		return errors.New("argumento vazio")
	}
	return nil
}

func isExecutable(p string) bool { return isExecutableOS(runtime.GOOS, p) }

// isExecutableOS: no Windows, arquivo .exe (.cmd/.bat/.ps1 exigem shell);
// nos outros, arquivo com bit de execução.
func isExecutableOS(goos, p string) bool {
	info, err := os.Stat(p)
	return err == nil && execFileOK(goos, p, info)
}

// execFileOK: no Windows o wt.exe costuma ser um alias de execução em
// WindowsApps (IO_REPARSE_TAG_APPEXECLINK), que o os.Stat marca Irregular;
// basta não ser diretório.
func execFileOK(goos, p string, info fs.FileInfo) bool {
	if goos == "windows" {
		return !info.IsDir() && strings.EqualFold(filepath.Ext(p), ".exe")
	}
	return info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}

// childEnv: no macOS, aberto pelo Finder, o app herda o PATH mínimo do
// launchd; a CLI instalada pelo npm (#!/usr/bin/env node) não acharia o
// node. Põe a pasta da CLI e as do Homebrew na frente do PATH.
func childEnv(goos string, env []string, cli string) []string {
	if goos != "darwin" {
		return env
	}
	out := slices.Clone(env)
	idx, cur := -1, []string(nil)
	for i, kv := range out {
		if v, ok := strings.CutPrefix(kv, "PATH="); ok {
			idx, cur = i, strings.Split(v, ":")
			break
		}
	}
	var dirs []string
	for _, d := range append([]string{path.Dir(cli), "/opt/homebrew/bin", "/usr/local/bin"}, cur...) {
		if d != "" && !slices.Contains(dirs, d) {
			dirs = append(dirs, d)
		}
	}
	kv := "PATH=" + strings.Join(dirs, ":")
	if idx < 0 {
		return append(out, kv)
	}
	out[idx] = kv
	return out
}

func isDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

// Known informa se name é um emulador com sintaxe conhecida.
func Known(name string) bool { _, ok := known[name]; return ok }
