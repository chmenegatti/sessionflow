// Package config resolve os diretórios da aplicação (XDG) e carrega/salva a
// configuração local em JSON.
package config

import (
	"errors"
	"path/filepath"
	"strings"

	"sessionflow/internal/domain"
)

const appDirName = "sessionflow"

// Env abstrai a leitura de variáveis de ambiente (os.Getenv em produção).
type Env func(string) string

// Paths são os diretórios e arquivos usados pelo SessionFlow.
type Paths struct {
	ConfigDir string `json:"configDir"`
	DataDir   string `json:"dataDir"`
	StateDir  string `json:"stateDir"`
	CacheDir  string `json:"cacheDir"`
}

// ResolvePaths devolve as pastas da aplicação no padrão de cada sistema
// (goos = runtime.GOOS; recebido como parâmetro para testar qualquer SO):
//   - linux: XDG Base Directory (variável absoluta vence; senão padrão no home);
//   - windows: %APPDATA%\SessionFlow (config) e %LOCALAPPDATA%\SessionFlow\{data,logs,cache};
//   - darwin: ~/Library/Application Support/SessionFlow (config e dados), ~/Library/Logs e ~/Library/Caches.
func ResolvePaths(goos string, env Env, home string) (Paths, error) {
	if !isAbs(goos, home) {
		return Paths{}, errors.New("diretório home inválido")
	}
	switch goos {
	case "windows":
		dir := func(key string, fallback ...string) string {
			if v := env(key); isAbs(goos, v) {
				return v
			}
			return join(goos, append([]string{home}, fallback...)...)
		}
		roaming := dir("APPDATA", "AppData", "Roaming")
		local := join(goos, dir("LOCALAPPDATA", "AppData", "Local"), winAppDir)
		return Paths{
			ConfigDir: join(goos, roaming, winAppDir),
			DataDir:   join(goos, local, "data"),
			StateDir:  join(goos, local, "logs"),
			CacheDir:  join(goos, local, "cache"),
		}, nil
	case "darwin":
		lib := join(goos, home, "Library")
		support := join(goos, lib, "Application Support", winAppDir)
		return Paths{
			ConfigDir: support,
			DataDir:   support,
			StateDir:  join(goos, lib, "Logs", winAppDir),
			CacheDir:  join(goos, lib, "Caches", winAppDir),
		}, nil
	}
	xdg := func(key, fallback string) string {
		if v := env(key); filepath.IsAbs(v) {
			return filepath.Join(v, appDirName)
		}
		return filepath.Join(home, fallback, appDirName)
	}
	return Paths{
		ConfigDir: xdg("XDG_CONFIG_HOME", ".config"),
		DataDir:   xdg("XDG_DATA_HOME", ".local/share"),
		StateDir:  xdg("XDG_STATE_HOME", ".local/state"),
		CacheDir:  xdg("XDG_CACHE_HOME", ".cache"),
	}, nil
}

// winAppDir é o nome da pasta no Windows e no macOS (convenção desses
// sistemas: nome do produto, não minúsculas).
const winAppDir = "SessionFlow"

// isAbs e join seguem as regras do sistema goos, não as do sistema em que
// o código roda (os testes montam caminhos do Windows no Linux).
func isAbs(goos, p string) bool {
	if goos != "windows" {
		return strings.HasPrefix(p, "/")
	}
	if strings.HasPrefix(p, `\\`) { // UNC: \\servidor\compartilhamento
		return true
	}
	return len(p) >= 3 && p[1] == ':' && (p[2] == '\\' || p[2] == '/') &&
		(p[0] >= 'A' && p[0] <= 'Z' || p[0] >= 'a' && p[0] <= 'z')
}

func join(goos string, parts ...string) string {
	if goos != "windows" {
		return filepath.ToSlash(filepath.Join(parts...))
	}
	out := ""
	for _, p := range parts {
		if p == "" {
			continue
		}
		if out == "" {
			out = strings.TrimRight(p, `\/`)
			continue
		}
		out += `\` + strings.Trim(p, `\/`)
	}
	return out
}

func (p Paths) ConfigFile() string { return filepath.Join(p.ConfigDir, "config.json") }
func (p Paths) IndexDB() string    { return filepath.Join(p.DataDir, "index.db") }
func (p Paths) UserDB() string     { return filepath.Join(p.DataDir, "user.db") }
func (p Paths) LogFile() string    { return filepath.Join(p.StateDir, "sessionflow.log") }

// DefaultProviderRoot devolve o diretório raiz padrão de cada provider,
// respeitando as variáveis documentadas (docs/discovery/02..04):
//   - Claude Code: $CLAUDE_CONFIG_DIR ou ~/.claude
//   - Codex:       $CODEX_HOME ou ~/.codex
//   - OpenCode:    $XDG_DATA_HOME/opencode ou ~/.local/share/opencode
//
// A interpretação do conteúdo (projects/, sessions/, opencode.db) é
// responsabilidade de cada adaptador.
func DefaultProviderRoot(p domain.ProviderID, env Env, home string) string {
	abs := func(key string) string {
		if v := env(key); filepath.IsAbs(v) {
			return filepath.Clean(v)
		}
		return ""
	}
	switch p {
	case domain.ProviderClaude:
		if v := abs("CLAUDE_CONFIG_DIR"); v != "" {
			return v
		}
		return filepath.Join(home, ".claude")
	case domain.ProviderCodex:
		if v := abs("CODEX_HOME"); v != "" {
			return v
		}
		return filepath.Join(home, ".codex")
	case domain.ProviderOpenCode:
		if v := abs("XDG_DATA_HOME"); v != "" {
			return filepath.Join(v, "opencode")
		}
		return filepath.Join(home, ".local", "share", "opencode")
	}
	return ""
}
