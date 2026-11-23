package domain

import (
	"errors"
	"path"
	"path/filepath"
	"strings"
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
//
// Caminho POSIX ("/...") vale em qualquer sistema e é limpo com as regras
// POSIX (no Windows, filepath.Clean trocaria "/" por "\": sessões vindas de
// Linux/WSL manteriam o caminho original). Caminho do Windows só vale no
// Windows.
func NormalizeProjectPath(p string) (string, error) {
	switch {
	case strings.HasPrefix(p, "/"):
		return path.Clean(p), nil
	case p != "" && filepath.IsAbs(p):
		return filepath.Clean(p), nil
	}
	return "", ErrRelativeProjectPath
}

// ProjectName devolve o nome exibido de um projeto (último segmento).
func ProjectName(path string) string {
	name := filepath.Base(path)
	if name == string(filepath.Separator) || name == "." {
		return path
	}
	return name
}
