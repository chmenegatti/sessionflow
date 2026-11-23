//go:build !unix

package lockfile

import "os"

// Windows/macOS ainda não são alvo (Fase 7): sem lock.
func lock(*os.File) error   { return nil }
func unlock(*os.File) error { return nil }
