package providertest

import (
	"path/filepath"
	"runtime"
)

// Exe é o caminho de um executável de teste em dir: no Windows o
// SessionFlow só aceita .exe (o resto exige shell).
func Exe(dir, name string) string {
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(dir, name)
}

// Abs é um caminho absoluto válido no sistema que roda o teste
// ("/a/b" no Linux e no macOS, `C:\a\b` no Windows).
func Abs(parts ...string) string {
	root := string(filepath.Separator)
	if runtime.GOOS == "windows" {
		root = `C:\`
	}
	return filepath.Join(append([]string{root}, parts...)...)
}
