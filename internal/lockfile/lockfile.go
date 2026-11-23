// Package lockfile garante uma única instância do SessionFlow por usuário:
// duas instâncias escreveriam no mesmo índice ao mesmo tempo.
package lockfile

import (
	"errors"
	"os"
	"path/filepath"
)

// ErrLocked indica que outra instância já segura o lock.
var ErrLocked = errors.New("outra instância do SessionFlow já está em execução")

// Lock é um lock exclusivo baseado em arquivo. O sistema o libera se o
// processo morrer.
type Lock struct{ f *os.File }

// Acquire tenta pegar o lock em path sem bloquear.
func Acquire(path string) (*Lock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := lock(f); err != nil {
		f.Close()
		return nil, err
	}
	return &Lock{f: f}, nil
}

// Release libera o lock.
func (l *Lock) Release() error {
	if l == nil || l.f == nil {
		return nil
	}
	err := unlock(l.f)
	return errors.Join(err, l.f.Close())
}
