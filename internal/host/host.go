// Package host cuida do ambiente em que o SessionFlow roda quando ele vem
// numa AppImage: o AppRun acrescenta ao ambiente caminhos de dentro da
// imagem (bibliotecas GTK, XDG_DATA_DIRS…), que um terminal ou CLI aberto
// pelo app não pode herdar, e o WebKit embutido depende de ferramentas de
// sandbox do sistema. Em instalação nativa (.deb, .rpm, binário) nada muda.
package host

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// Env descreve o ambiente; Detect preenche a partir do processo atual.
type Env struct {
	AppDir  string // raiz montada da AppImage ("" = não é AppImage)
	environ func() []string
	stat    func(string) (fs.FileInfo, error)
}

// Current é o ambiente deste processo (detectado uma vez: é um fato do
// processo, como os.Getenv).
var Current = Detect()

// Detect inspeciona o processo atual.
func Detect() Env {
	e := Env{environ: os.Environ, stat: os.Stat}
	if os.Getenv("APPIMAGE") != "" {
		e.AppDir = filepath.Clean(os.Getenv("APPDIR"))
	}
	return e
}

// Command prepara argv para rodar em dir, sem shell e sem o ambiente
// injetado pela AppImage.
func (e Env) Command(argv []string, dir string) *exec.Cmd {
	c := exec.Command(argv[0], argv[1:]...)
	c.Dir = dir
	c.Env = e.CleanEnviron()
	return c
}

// appImageVars: variáveis que o AppRun (linuxdeploy, plugin GTK) define
// apontando para dentro da imagem.
var appImageVars = []string{
	"APPDIR", "APPIMAGE", "ARGV0", "OWD", "LD_LIBRARY_PATH", "LD_PRELOAD", "GDK_PIXBUF_MODULE_FILE",
	"GDK_PIXBUF_MODULEDIR", "GIO_EXTRA_MODULES", "GIO_MODULE_DIR", "GSETTINGS_SCHEMA_DIR", "GTK_DATA_PREFIX",
	"GTK_EXE_PREFIX", "GTK_PATH", "GTK_IM_MODULE_FILE", "GTK_THEME", "GST_PLUGIN_SYSTEM_PATH",
	"WEBKIT_EXEC_PATH", "WEBKIT_INJECTED_BUNDLE_PATH", "PYTHONHOME", "PYTHONPATH", "PERLLIB",
}

// CleanEnviron devolve o ambiente sem o que o AppImage injetou: variáveis
// próprias dele e, nas listas de caminhos (PATH, XDG_DATA_DIRS…), as
// entradas de dentro da imagem. Fora de AppImage, o ambiente como está.
func (e Env) CleanEnviron() []string {
	env := e.environ()
	if e.AppDir == "" || e.AppDir == "." || e.AppDir == "/" {
		return env
	}
	inside := func(p string) bool { return p == e.AppDir || strings.HasPrefix(p, e.AppDir+"/") }
	out := make([]string, 0, len(env))
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		if slices.Contains(appImageVars, k) {
			continue
		}
		if strings.Contains(v, e.AppDir) {
			parts := slices.DeleteFunc(strings.Split(v, ":"), inside)
			if len(parts) == 0 {
				continue
			}
			v = strings.Join(parts, ":")
		}
		out = append(out, k+"="+v)
	}
	return out
}

// sandboxTools: o WebKitGTK roda o processo web dentro de um sandbox
// bubblewrap e procura estes programas em caminhos fixos da compilação;
// não dá para levá-los dentro da AppImage.
var sandboxTools = []string{"/usr/bin/bwrap", "/usr/bin/xdg-dbus-proxy"}

// MissingWebKitTools lista o que falta no sistema para o WebKit de uma
// AppImage abrir a janela. Pacotes nativos declaram essas dependências,
// então lá a lista é sempre vazia.
func (e Env) MissingWebKitTools() []string {
	if e.AppDir == "" {
		return nil
	}
	var out []string
	for _, p := range sandboxTools {
		if info, err := e.stat(p); err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
			out = append(out, p)
		}
	}
	return out
}
