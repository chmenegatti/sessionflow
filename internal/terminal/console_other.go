//go:build !windows

package terminal

import "os/exec"

func newConsole(*exec.Cmd) {}
