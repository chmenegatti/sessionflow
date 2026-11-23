package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sessionflow/internal/domain"
)

func envOf(m map[string]string) Env { return func(k string) string { return m[k] } }

func TestResolvePathsDefaults(t *testing.T) {
	p, err := ResolvePaths(envOf(nil), "/home/u")
	if err != nil {
		t.Fatal(err)
	}
	want := Paths{
		ConfigDir: "/home/u/.config/sessionflow",
		DataDir:   "/home/u/.local/share/sessionflow",
		StateDir:  "/home/u/.local/state/sessionflow",
		CacheDir:  "/home/u/.cache/sessionflow",
	}
	if p != want {
		t.Fatalf("got %+v", p)
	}
	if p.IndexDB() != "/home/u/.local/share/sessionflow/index.db" || p.UserDB() != "/home/u/.local/share/sessionflow/user.db" {
		t.Errorf("bancos: %s %s", p.IndexDB(), p.UserDB())
	}
	if p.ConfigFile() != "/home/u/.config/sessionflow/config.json" || p.LogFile() != "/home/u/.local/state/sessionflow/sessionflow.log" {
		t.Errorf("arquivos: %s %s", p.ConfigFile(), p.LogFile())
	}
}

func TestResolvePathsXDGOverrides(t *testing.T) {
	p, _ := ResolvePaths(envOf(map[string]string{
		"XDG_CONFIG_HOME": "/x/cfg",
		"XDG_DATA_HOME":   "relativo/ignorado", // spec XDG: caminho relativo deve ser ignorado
	}), "/home/u")
	if p.ConfigDir != "/x/cfg/sessionflow" {
		t.Errorf("ConfigDir %s", p.ConfigDir)
	}
	if p.DataDir != "/home/u/.local/share/sessionflow" {
		t.Errorf("DataDir %s", p.DataDir)
	}
}

func TestResolvePathsRejectsRelativeHome(t *testing.T) {
	if _, err := ResolvePaths(envOf(nil), "home"); err == nil {
		t.Fatal("esperava erro")
	}
}

func TestDefaultProviderRoot(t *testing.T) {
	home := "/home/u"
	cases := []struct {
		p    domain.ProviderID
		env  map[string]string
		want string
	}{
		{domain.ProviderClaude, nil, "/home/u/.claude"},
		{domain.ProviderClaude, map[string]string{"CLAUDE_CONFIG_DIR": "/opt/claude/"}, "/opt/claude"},
		{domain.ProviderCodex, nil, "/home/u/.codex"},
		{domain.ProviderCodex, map[string]string{"CODEX_HOME": "/srv/codex"}, "/srv/codex"},
		{domain.ProviderCodex, map[string]string{"CODEX_HOME": "rel"}, "/home/u/.codex"},
		{domain.ProviderOpenCode, nil, "/home/u/.local/share/opencode"},
		{domain.ProviderOpenCode, map[string]string{"XDG_DATA_HOME": "/d"}, "/d/opencode"},
	}
	for _, c := range cases {
		if got := DefaultProviderRoot(c.p, envOf(c.env), home); got != c.want {
			t.Errorf("%s %v: got %q quer %q", c.p, c.env, got, c.want)
		}
	}
}

func TestLoadMissingReturnsDefault(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "nao-existe.json"))
	if err != nil {
		t.Fatal(err)
	}
	if c.LogLevel != "info" || len(c.Providers) != 3 || !c.Providers[domain.ProviderCodex].Enabled {
		t.Fatalf("default inesperado: %+v", c)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.json")
	c := Default()
	c.LogLevel = "debug"
	c.Providers[domain.ProviderOpenCode] = ProviderConfig{Enabled: false, RootDir: "/custom/oc", CLIPath: "/usr/bin/opencode"}
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("permissão %v", info.Mode().Perm())
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.SlogLevel() != slog.LevelDebug || got.Providers[domain.ProviderOpenCode] != c.Providers[domain.ProviderOpenCode] {
		t.Fatalf("got %+v", got)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Errorf("arquivo temporário sobrou: %v", entries)
	}
}

func TestLoadFillsMissingProviders(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	os.WriteFile(path, []byte(`{"logLevel":"warn","providers":{"claude":{"enabled":false}}}`), 0o600)
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Providers[domain.ProviderClaude].Enabled || !c.Providers[domain.ProviderCodex].Enabled {
		t.Fatalf("got %+v", c.Providers)
	}
}

func TestLoadInvalid(t *testing.T) {
	cases := map[string]string{
		"json quebrado":     `{"logLevel":`,
		"nível inválido":    `{"logLevel":"verbose"}`,
		"provider estranho": `{"logLevel":"info","providers":{"gemini":{"enabled":true}}}`,
		"cli relativo":      `{"logLevel":"info","providers":{"codex":{"enabled":true,"cliPath":"bin/codex"}}}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			os.WriteFile(path, []byte(body), 0o600)
			c, err := Load(path)
			if err == nil {
				t.Fatal("esperava erro")
			}
			if c.LogLevel != "info" || len(c.Providers) != 3 {
				t.Errorf("erro deve devolver Default, veio %+v", c)
			}
		})
	}
}

func TestSaveRejectsInvalid(t *testing.T) {
	c := Default()
	c.LogLevel = "nope"
	err := Save(filepath.Join(t.TempDir(), "c.json"), c)
	if err == nil || !strings.Contains(err.Error(), "logLevel") {
		t.Fatalf("got %v", err)
	}
}

// Arquivo editado à mão com valores nulos ou parciais não derruba o app.
func TestLoadToleratesNullAndPartialProviders(t *testing.T) {
	cases := map[string]string{
		"providers null": `{"logLevel":"info","providers":null}`,
		"sem providers":  `{"logLevel":"info"}`,
		"só rootDir":     `{"logLevel":"info","providers":{"claude":{"rootDir":"/x"}}}`,
		"objeto vazio":   `{}`,
		"provider vazio": `{"providers":{"codex":{}}}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			os.WriteFile(path, []byte(body), 0o600)
			c, err := Load(path)
			if err != nil {
				t.Fatal(err)
			}
			for _, p := range domain.AllProviders() {
				if !c.Providers[p].Enabled {
					t.Errorf("%s deveria estar habilitado: %+v", p, c.Providers)
				}
			}
		})
	}
}

func TestConfigDefaultCurrency(t *testing.T) {
	if Default().DefaultCurrency != "BRL" {
		t.Errorf("padrão %q", Default().DefaultCurrency)
	}
	c := Default()
	c.DefaultCurrency = "real"
	if c.Validate() == nil {
		t.Error("moeda inválida aceita")
	}
	// Arquivo anterior à Fase 9 (sem o campo) fica com BRL.
	p := filepath.Join(t.TempDir(), "config.json")
	os.WriteFile(p, []byte(`{"logLevel":"info"}`), 0o600)
	got, err := Load(p)
	if err != nil || got.DefaultCurrency != "BRL" {
		t.Errorf("%q %v", got.DefaultCurrency, err)
	}
}

// Moeda editada à mão não pode invalidar as outras preferências.
func TestLoadCurrencyHandEdited(t *testing.T) {
	dir := t.TempDir()
	for in, want := range map[string]string{`"usd"`: "USD", `" eur "`: "EUR", `"real"`: "BRL", `""`: "BRL"} {
		p := filepath.Join(dir, "c.json")
		os.WriteFile(p, []byte(`{"logLevel":"debug","defaultCurrency":`+in+`}`), 0o600)
		c, err := Load(p)
		if err != nil || c.DefaultCurrency != want || c.LogLevel != "debug" {
			t.Errorf("%s → %q, log %q, %v", in, c.DefaultCurrency, c.LogLevel, err)
		}
	}
}
