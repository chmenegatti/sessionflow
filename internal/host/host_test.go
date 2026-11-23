package host

import (
	"io/fs"
	"os"
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

func TestNativeCommand(t *testing.T) {
	e := Env{environ: func() []string { return []string{"A=1"} }, stat: os.Stat}
	c := e.Command([]string{"/bin/true", "x"}, "/tmp")
	if c.Path != "/bin/true" || !slices.Equal(c.Args, []string{"/bin/true", "x"}) || c.Dir != "/tmp" || !slices.Equal(c.Env, []string{"A=1"}) {
		t.Errorf("%+v", c)
	}
}

// Terminal aberto de dentro de um AppImage não herda o que o AppRun
// injetou.
func TestCleanEnvironDropsAppImage(t *testing.T) {
	e := Env{AppDir: "/tmp/.mount_SessiX", environ: func() []string {
		return []string{
			"HOME=/home/u",
			"APPIMAGE=/home/u/SessionFlow.AppImage",
			"LD_LIBRARY_PATH=/tmp/.mount_SessiX/usr/lib",
			"GTK_PATH=/tmp/.mount_SessiX/usr/lib/gtk-4.0",
			"XDG_DATA_DIRS=/tmp/.mount_SessiX/usr/share:/usr/local/share:/usr/share",
			"PATH=/tmp/.mount_SessiX/usr/bin:/usr/bin:/bin",
			"GSETTINGS_SCHEMA_DIR=/tmp/.mount_SessiX/usr/share/glib-2.0/schemas",
			"SOME_VAR=/tmp/.mount_SessiX",
			"KEEP=/tmp/.mount_SessiXY/a", // outro diretório com o mesmo prefixo
			"TERM=xterm-256color",
		}
	}}
	got := e.CleanEnviron()
	want := []string{"HOME=/home/u", "XDG_DATA_DIRS=/usr/local/share:/usr/share", "PATH=/usr/bin:/bin", "KEEP=/tmp/.mount_SessiXY/a", "TERM=xterm-256color"}
	if !slices.Equal(got, want) {
		t.Errorf("\n got %q\nwant %q", got, want)
	}
	// Fora de AppImage: ambiente intacto.
	e.AppDir = ""
	if len(e.CleanEnviron()) != 10 {
		t.Error("alterou ambiente fora de AppImage")
	}
}

func TestMissingWebKitTools(t *testing.T) {
	files := map[string]fs.FileMode{"/usr/bin/bwrap": 0o755}
	e := Env{AppDir: "/tmp/.mount_x", stat: func(p string) (fs.FileInfo, error) {
		if m, ok := files[p]; ok {
			return fakeInfo{m}, nil
		}
		return nil, os.ErrNotExist
	}}
	if got := e.MissingWebKitTools(); !slices.Equal(got, []string{"/usr/bin/xdg-dbus-proxy"}) {
		t.Errorf("AppImage: %v", got)
	}
	e.AppDir = ""
	if got := e.MissingWebKitTools(); got != nil {
		t.Errorf("fora de AppImage: %v", got)
	}
}
