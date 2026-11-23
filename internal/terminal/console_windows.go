//go:build windows

package terminal

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// newConsole: o comando ganha uma janela de console própria (sem cmd.exe).
func newConsole(p *exec.Cmd) {
	if p.SysProcAttr == nil {
		p.SysProcAttr = &syscall.SysProcAttr{}
	}
	p.SysProcAttr.CreationFlags |= windows.CREATE_NEW_CONSOLE
}
