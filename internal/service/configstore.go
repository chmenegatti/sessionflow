package service

import (
	"errors"
	"maps"
	"slices"
	"sync"

	"sessionflow/internal/config"
)

// ErrConfigUnreadable impede gravar por cima de um config.json que o app
// não conseguiu ler (o usuário perderia o que escreveu à mão).
var ErrConfigUnreadable = errors.New("config.json está inválido; corrija o arquivo à mão antes de salvar pela interface")

// ConfigStore guarda a configuração em uso e a grava no arquivo. Serviços
// leem a versão atual a cada uso (caminho da CLI e terminal valem na hora).
type ConfigStore struct {
	mu       sync.RWMutex
	path     string
	cfg      config.Config
	writable bool
}

// NewConfigStore usa cfg (já carregada). loadErr != nil = o arquivo existe
// mas não pôde ser lido: a interface não grava por cima dele.
func NewConfigStore(path string, cfg config.Config, loadErr error) *ConfigStore {
	return &ConfigStore{path: path, cfg: cfg, writable: loadErr == nil}
}

// Get devolve uma cópia da configuração atual.
func (s *ConfigStore) Get() config.Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return clone(s.cfg)
}

// Update aplica fn numa cópia, valida, grava e só então passa a usá-la.
func (s *ConfigStore) Update(fn func(*config.Config) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.writable {
		return ErrConfigUnreadable
	}
	next := clone(s.cfg)
	if err := fn(&next); err != nil {
		return err
	}
	if err := config.Save(s.path, next); err != nil {
		return err
	}
	s.cfg = next
	return nil
}

func clone(c config.Config) config.Config {
	c.Providers = maps.Clone(c.Providers)
	c.Terminal.Args = slices.Clone(c.Terminal.Args)
	return c
}
