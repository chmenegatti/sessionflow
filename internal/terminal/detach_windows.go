//go:build windows

package terminal

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// detach: o processo não recebe o Ctrl+C nem fecha junto com o app.
func detach(p *exec.Cmd) {
	if p.SysProcAttr == nil {
		p.SysProcAttr = &syscall.SysProcAttr{}
	}
	p.SysProcAttr.CreationFlags |= windows.CREATE_NEW_PROCESS_GROUP
}
