//go:build unix

package terminal

import (
	"os/exec"
	"syscall"
)

// detach coloca o emulador numa sessão própria: sinais do app (Ctrl+C no
// terminal que o iniciou, encerramento) não chegam a ele.
func detach(c *exec.Cmd) { c.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }
