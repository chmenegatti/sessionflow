package config

import "testing"

// Pastas da aplicação por sistema (design 0.2.0).
func TestResolvePathsByOS(t *testing.T) {
	winEnv := map[string]string{
		"APPDATA":       `C:\Users\José Silva\AppData\Roaming`,
		"LOCALAPPDATA":  `C:\Users\José Silva\AppData\Local`,
		"XDG_DATA_HOME": "/ignorado",
	}
	for _, tc := range []struct {
		name, goos, home string
		env              map[string]string
		want             Paths
	}{
		{"windows", "windows", `C:\Users\José Silva`, winEnv, Paths{
			ConfigDir: `C:\Users\José Silva\AppData\Roaming\SessionFlow`,
			DataDir:   `C:\Users\José Silva\AppData\Local\SessionFlow\data`,
			StateDir:  `C:\Users\José Silva\AppData\Local\SessionFlow\logs`,
			CacheDir:  `C:\Users\José Silva\AppData\Local\SessionFlow\cache`,
		}},
		{"windows sem APPDATA", "windows", `C:\Users\ana`, nil, Paths{
			ConfigDir: `C:\Users\ana\AppData\Roaming\SessionFlow`,
			DataDir:   `C:\Users\ana\AppData\Local\SessionFlow\data`,
			StateDir:  `C:\Users\ana\AppData\Local\SessionFlow\logs`,
			CacheDir:  `C:\Users\ana\AppData\Local\SessionFlow\cache`,
		}},
		{"windows APPDATA relativo", "windows", `C:\Users\ana`, map[string]string{"APPDATA": `AppData`}, Paths{
			ConfigDir: `C:\Users\ana\AppData\Roaming\SessionFlow`,
			DataDir:   `C:\Users\ana\AppData\Local\SessionFlow\data`,
			StateDir:  `C:\Users\ana\AppData\Local\SessionFlow\logs`,
			CacheDir:  `C:\Users\ana\AppData\Local\SessionFlow\cache`,
		}},
		{"macos", "darwin", "/Users/José Silva", map[string]string{"XDG_CONFIG_HOME": "/x"}, Paths{
			ConfigDir: "/Users/José Silva/Library/Application Support/SessionFlow",
			DataDir:   "/Users/José Silva/Library/Application Support/SessionFlow",
			StateDir:  "/Users/José Silva/Library/Logs/SessionFlow",
			CacheDir:  "/Users/José Silva/Library/Caches/SessionFlow",
		}},
		{"linux", "linux", "/home/u", nil, Paths{
			ConfigDir: "/home/u/.config/sessionflow",
			DataDir:   "/home/u/.local/share/sessionflow",
			StateDir:  "/home/u/.local/state/sessionflow",
			CacheDir:  "/home/u/.cache/sessionflow",
		}},
	} {
		got, err := ResolvePaths(tc.goos, envOf(tc.env), tc.home)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got != tc.want {
			t.Errorf("%s:\n got %+v\nwant %+v", tc.name, got, tc.want)
		}
	}
}

func TestResolvePathsRejectsRelativeHomeByOS(t *testing.T) {
	for goos, home := range map[string]string{"windows": `Users\ana`, "darwin": "Users/ana"} {
		if _, err := ResolvePaths(goos, envOf(nil), home); err == nil {
			t.Errorf("%s: home relativo aceito", goos)
		}
	}
	if _, err := ResolvePaths("windows", envOf(nil), `\\servidor\home\ana`); err != nil {
		t.Errorf("caminho UNC recusado: %v", err)
	}
}
