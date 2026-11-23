//go:build windows

package terminal

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// startConsole abre argv numa janela de console própria, sem cmd.exe. Usa
// CreateProcess direto: o exec.Cmd sempre passa STARTF_USESTDHANDLES (com
// NUL quando não há entrada e saída), e a CLI não enxergaria o console.
func startConsole(argv []string, dir string) error {
	app, err := windows.UTF16PtrFromString(argv[0])
	if err != nil {
		return err
	}
	line, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(argv))
	if err != nil {
		return err
	}
	wd, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return err
	}
	si := windows.StartupInfo{Cb: uint32(unsafe.Sizeof(windows.StartupInfo{}))}
	var pi windows.ProcessInformation
	if err := windows.CreateProcess(app, line, nil, nil, false, windows.CREATE_NEW_CONSOLE, nil, wd, &si, &pi); err != nil {
		return err
	}
	windows.CloseHandle(pi.Thread)  //nolint:errcheck // só libera o handle
	windows.CloseHandle(pi.Process) //nolint:errcheck // o console segue sozinho
	return nil
}
