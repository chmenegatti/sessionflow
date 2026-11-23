package terminal

import (
	"io/fs"
	"slices"
	"testing"
	"time"
)

type fakeInfo struct{ mode fs.FileMode }

func (f fakeInfo) Name() string       { return "" }
func (f fakeInfo) Size() int64        { return 0 }
func (f fakeInfo) Mode() fs.FileMode  { return f.mode }
func (f fakeInfo) ModTime() time.Time { return time.Time{} }
func (f fakeInfo) IsDir() bool        { return f.mode.IsDir() }
func (f fakeInfo) Sys() any           { return nil }

// No Windows o wt.exe é um alias de execução em WindowsApps
// (IO_REPARSE_TAG_APPEXECLINK); o os.Stat do Go 1.23+ o marca Irregular.
func TestExecFileOKWindowsAppExecLink(t *testing.T) {
	wt := `C:\Users\u\AppData\Local\Microsoft\WindowsApps\wt.exe`
	if !execFileOK("windows", wt, fakeInfo{fs.ModeIrregular}) {
		t.Error("alias de execução recusado")
	}
	if execFileOK("windows", wt, fakeInfo{fs.ModeDir}) {
		t.Error("diretório aceito")
	}
	if execFileOK("windows", `C:\x\wt.cmd`, fakeInfo{0}) {
		t.Error(".cmd aceito")
	}
	if execFileOK("linux", "/x/kitty", fakeInfo{fs.ModeIrregular | 0o755}) {
		t.Error("irregular aceito fora do Windows")
	}
}

// Aberto pelo Finder, o app herda o PATH mínimo do launchd: a CLI do npm
// (#!/usr/bin/env node) não acharia o node.
func TestChildEnvDarwinPath(t *testing.T) {
	env := []string{"A=1", "PATH=/usr/bin:/bin"}
	got := childEnv("darwin", env, "/Users/u/.nvm/versions/node/v22/bin/codex")
	want := []string{"A=1", "PATH=/Users/u/.nvm/versions/node/v22/bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin"}
	if !slices.Equal(got, want) {
		t.Errorf("got %q\nquer %q", got, want)
	}
	got = childEnv("darwin", []string{"PATH=/opt/homebrew/bin:/usr/bin"}, "/opt/homebrew/bin/codex")
	if want := []string{"PATH=/opt/homebrew/bin:/usr/local/bin:/usr/bin"}; !slices.Equal(got, want) {
		t.Errorf("sem repetir: got %q", got)
	}
	if got := childEnv("darwin", []string{"A=1"}, "/opt/homebrew/bin/codex"); !slices.Equal(got, []string{"A=1", "PATH=/opt/homebrew/bin:/usr/local/bin"}) {
		t.Errorf("sem PATH: got %q", got)
	}
	if got := childEnv("linux", env, "/x/codex"); !slices.Equal(got, env) {
		t.Errorf("linux mudou: %q", got)
	}
}
