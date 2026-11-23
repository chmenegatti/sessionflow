// Package providertest tem utilitários de teste para adaptadores.
package providertest

import (
	"path/filepath"
	"runtime"

	"sessionflow/internal/domain"
)

// Sink grava em memória tudo o que o adaptador entrega.
type Sink struct {
	Messages map[domain.SessionKey][]domain.Message
	Sessions []domain.Session
	Usages   map[domain.SessionKey][]domain.Usage
}

func NewSink() *Sink {
	return &Sink{Messages: map[domain.SessionKey][]domain.Message{}, Usages: map[domain.SessionKey][]domain.Usage{}}
}

// Usage implementa provider.UsageSink.
func (s *Sink) Usage(k domain.SessionKey, u domain.Usage) error {
	s.Usages[k] = append(s.Usages[k], u)
	return nil
}

func (s *Sink) Message(k domain.SessionKey, m domain.Message) error {
	s.Messages[k] = append(s.Messages[k], m)
	return nil
}

func (s *Sink) Session(x domain.Session) error {
	s.Sessions = append(s.Sessions, x)
	return nil
}

// Fixture devolve o caminho de um arquivo em docs/discovery/fixtures.
func Fixture(parts ...string) string {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "..", "..", "docs", "discovery", "fixtures")
	return filepath.Join(append([]string{root}, parts...)...)
}
