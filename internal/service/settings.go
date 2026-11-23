package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"sessionflow/internal/config"
	"sessionflow/internal/domain"
	"sessionflow/internal/provider"
	"sessionflow/internal/terminal"
)

// Erros de validação das configurações exibidos ao usuário.
var (
	ErrPathNotAbsolute = errors.New("informe um caminho absoluto (começando com /)")
	ErrNotExecutable   = errors.New("o arquivo não existe ou não é executável")
	ErrNotDirectory    = errors.New("o diretório não existe")
)

// ProviderSettings é a configuração editável de um provider.
type ProviderSettings struct {
	ID          domain.ProviderID `json:"id"`
	Name        string            `json:"name"`
	Enabled     bool              `json:"enabled"`
	RootDir     string            `json:"rootDir"`     // configurado ("" = padrão)
	DefaultRoot string            `json:"defaultRoot"` // padrão do ambiente
	CLIPath     string            `json:"cliPath"`     // configurado ("" = procurar)
	DetectedCLI string            `json:"detectedCli"` // o que a retomada usaria agora ("" = nenhum)
}

// TerminalSettings é o emulador usado na retomada.
type TerminalSettings struct {
	Path     string   `json:"path"` // configurado ("" = detectar)
	Args     []string `json:"args"`
	Resolved string   `json:"resolved"` // emulador que seria usado agora
	Error    string   `json:"error"`    // por que não há emulador
}

// SettingsView reúne o que a tela de Configurações edita.
type SettingsView struct {
	ConfigFile      string             `json:"configFile"`
	Writable        bool               `json:"writable"`        // false = config.json inválido
	RestartRequired bool               `json:"restartRequired"` // diretório ou habilitação mudou
	Providers       []ProviderSettings `json:"providers"`
	Terminal        TerminalSettings   `json:"terminal"`
}

// SettingsService lê e grava config.json. Executável e terminal valem na
// hora; diretório de dados e habilitação, ao reiniciar (adaptadores e
// watcher são montados no início).
type SettingsService struct {
	store    *ConfigStore
	startup  config.Config
	env      ProviderEnv
	resumers map[domain.ProviderID]provider.Resumer
	look     terminal.LookPath
	file     string
}

// NewSettingsService cria o serviço. providers são os adaptadores ativos.
func NewSettingsService(store *ConfigStore, env ProviderEnv, providers []provider.Provider, look terminal.LookPath) *SettingsService {
	m := map[domain.ProviderID]provider.Resumer{}
	for _, p := range providers {
		if r, ok := p.(provider.Resumer); ok {
			m[p.ID()] = r
		}
	}
	return &SettingsService{store: store, startup: store.Get(), env: env, resumers: m, look: look, file: env.Paths.ConfigFile()}
}

// Get devolve a configuração atual com o que foi detectado.
func (s *SettingsService) Get(context.Context) SettingsView {
	cfg := s.store.Get()
	v := SettingsView{ConfigFile: s.file, Writable: s.store.writable}
	for _, id := range domain.AllProviders() {
		pc, old := cfg.Providers[id], s.startup.Providers[id]
		ps := ProviderSettings{
			ID: id, Name: id.DisplayName(), Enabled: pc.Enabled, RootDir: pc.RootDir, CLIPath: pc.CLIPath,
			DefaultRoot: config.DefaultProviderRoot(id, s.env.Env, s.env.Home),
		}
		if r, ok := s.resumers[id]; ok {
			ps.DetectedCLI = r.CLI(pc.CLIPath)
		}
		if pc.Enabled != old.Enabled || pc.RootDir != old.RootDir {
			v.RestartRequired = true
		}
		v.Providers = append(v.Providers, ps)
	}
	v.Terminal = TerminalSettings{Path: cfg.Terminal.Path, Args: nonNil(cfg.Terminal.Args)}
	if t, err := terminal.Resolve(cfg.Terminal, s.look); err != nil {
		v.Terminal.Error = err.Error()
	} else {
		v.Terminal.Resolved = t.Path
	}
	return v
}

// SetCLIPath define o executável do provider ("" = procurar).
func (s *SettingsService) SetCLIPath(_ context.Context, id domain.ProviderID, path string) error {
	if !id.Valid() {
		return ErrInvalidFilter
	}
	path = strings.TrimSpace(path)
	if path != "" {
		if !filepath.IsAbs(path) {
			return ErrPathNotAbsolute
		}
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
			return ErrNotExecutable
		}
		path = filepath.Clean(path)
	}
	return s.store.Update(func(c *config.Config) error {
		pc := c.Providers[id]
		pc.CLIPath = path
		c.Providers[id] = pc
		return nil
	})
}

// SetProvider habilita/desabilita o provider e define o diretório de
// dados ("" = padrão). Vale ao reiniciar.
func (s *SettingsService) SetProvider(_ context.Context, id domain.ProviderID, enabled bool, rootDir string) error {
	if !id.Valid() {
		return ErrInvalidFilter
	}
	rootDir = strings.TrimSpace(rootDir)
	if rootDir != "" {
		if !filepath.IsAbs(rootDir) {
			return ErrPathNotAbsolute
		}
		if info, err := os.Stat(rootDir); err != nil || !info.IsDir() {
			return ErrNotDirectory
		}
		rootDir = filepath.Clean(rootDir)
	}
	return s.store.Update(func(c *config.Config) error {
		pc := c.Providers[id]
		pc.Enabled, pc.RootDir = enabled, rootDir
		c.Providers[id] = pc
		return nil
	})
}

// SetTerminal define o emulador ("" = detectar) e, opcionalmente, os
// argumentos com {dir} e {cmd}.
func (s *SettingsService) SetTerminal(_ context.Context, path string, args []string) error {
	path = strings.TrimSpace(path)
	args = slices.DeleteFunc(slices.Clone(args), func(a string) bool { return strings.TrimSpace(a) == "" })
	t := terminal.Config{Path: path, Args: args}
	if path == "" && len(args) > 0 {
		return ErrPathNotAbsolute
	}
	if path != "" {
		if !filepath.IsAbs(path) {
			return ErrPathNotAbsolute
		}
		if _, err := terminal.Resolve(t, s.look); err != nil {
			return err
		}
		t.Path = filepath.Clean(path)
	}
	return s.store.Update(func(c *config.Config) error {
		c.Terminal = t
		return nil
	})
}
