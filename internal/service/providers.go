package service

import (
	"path/filepath"
	"runtime"

	"sessionflow/internal/config"
	"sessionflow/internal/domain"
	"sessionflow/internal/provider"
	"sessionflow/internal/provider/claude"
	"sessionflow/internal/provider/codex"
	"sessionflow/internal/provider/opencode"
)

// ProviderEnv reúne o que os adaptadores precisam do ambiente.
type ProviderEnv struct {
	Config   config.Config
	Env      config.Env
	Home     string
	Paths    config.Paths
	LookPath provider.LookPath
}

// SnapshotDir é onde cópias de bancos de terceiros são lidas.
func (e ProviderEnv) SnapshotDir() string { return filepath.Join(e.Paths.CacheDir, "snapshots") }

// Root devolve o diretório de dados de um provider: o configurado ou o
// padrão do ambiente.
func (e ProviderEnv) Root(id domain.ProviderID) string {
	if r := e.Config.Providers[id].RootDir; r != "" {
		return r
	}
	return config.DefaultProviderRoot(runtime.GOOS, id, e.Env, e.Home)
}

// Disabled devolve os providers desabilitados na configuração.
func (e ProviderEnv) Disabled() []domain.ProviderID {
	var out []domain.ProviderID
	for _, id := range domain.AllProviders() {
		if !e.Config.Providers[id].Enabled {
			out = append(out, id)
		}
	}
	return out
}

// NewProviders cria os adaptadores dos providers habilitados, na ordem de
// domain.AllProviders.
func NewProviders(e ProviderEnv) []provider.Provider {
	var out []provider.Provider
	for _, id := range domain.AllProviders() {
		pc := e.Config.Providers[id]
		if !pc.Enabled {
			continue
		}
		root := e.Root(id)
		switch id {
		case domain.ProviderClaude:
			out = append(out, claude.New(claude.Options{RootDir: root, CLIPath: pc.CLIPath, Home: e.Home, LookPath: e.LookPath}))
		case domain.ProviderCodex:
			out = append(out, codex.New(codex.Options{RootDir: root, CLIPath: pc.CLIPath, Home: e.Home, LookPath: e.LookPath}))
		case domain.ProviderOpenCode:
			out = append(out, opencode.New(opencode.Options{
				RootDir: root, DBOverride: e.Env("OPENCODE_DB"), CLIPath: pc.CLIPath, Home: e.Home,
				LookPath: e.LookPath, SnapshotDir: e.SnapshotDir(),
			}))
		}
	}
	return out
}
