//go:build !windows

package terminal

import "errors"

func startConsole([]string, string) error { return errors.New("console: só no Windows") }
