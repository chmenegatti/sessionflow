//go:build !unix && !windows

package lockfile

import "os"

// Outros sistemas (nenhum distribuído): sem lock.
func lock(*os.File) error   { return nil }
func unlock(*os.File) error { return nil }
