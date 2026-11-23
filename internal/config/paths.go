// Package config resolve os diretórios da aplicação (XDG) e carrega/salva a
// configuração local em JSON.
package config

import (
	"errors"
	"path/filepath"

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

// ResolvePaths aplica a XDG Base Directory Specification: variável definida e
// absoluta vence; caso contrário usa o padrão relativo a home.
func ResolvePaths(env Env, home string) (Paths, error) {
	if !filepath.IsAbs(home) {
		return Paths{}, errors.New("diretório home inválido")
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
