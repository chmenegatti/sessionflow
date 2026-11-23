//go:build !unix && !windows

package terminal

import "os/exec"

func detach(*exec.Cmd) {}
