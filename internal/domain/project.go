package domain

import (
	"errors"
	"path/filepath"
	"time"
)

// ErrRelativeProjectPath indica caminho de projeto não absoluto.
var ErrRelativeProjectPath = errors.New("caminho de projeto deve ser absoluto")

// Project agrupa sessões de um mesmo diretório, de qualquer provider.
type Project struct {
	Path         string       `json:"path"`
	Name         string       `json:"name"`
	Providers    []ProviderID `json:"providers"`
	SessionCount int          `json:"sessionCount"`
	LastActivity time.Time    `json:"lastActivity"`
}

// NormalizeProjectPath limpa o caminho para que o mesmo diretório vindo de
// providers diferentes produza a mesma chave de projeto.
func NormalizeProjectPath(p string) (string, error) {
	if p == "" || !filepath.IsAbs(p) {
		return "", ErrRelativeProjectPath
	}
	return filepath.Clean(p), nil
}

// ProjectName devolve o nome exibido de um projeto (último segmento).
func ProjectName(path string) string {
	name := filepath.Base(path)
	if name == string(filepath.Separator) || name == "." {
		return path
	}
	return name
}
