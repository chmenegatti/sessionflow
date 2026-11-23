//go:build !unix

package terminal

import "os/exec"

func detach(*exec.Cmd) {}
