package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"sessionflow/internal/domain"
	"sessionflow/internal/terminal"
)

// ProviderConfig são as preferências de um provider.
// Campos vazios = usar o padrão detectado.
type ProviderConfig struct {
	Enabled bool   `json:"enabled"`
	RootDir string `json:"rootDir,omitempty"`
	CLIPath string `json:"cliPath,omitempty"`
}

// UnmarshalJSON aplica o padrão Enabled=true quando "enabled" é omitido:
// quem edita o arquivo só para definir rootDir não desabilita o provider.
func (p *ProviderConfig) UnmarshalJSON(b []byte) error {
	type plain ProviderConfig // sem o método, evita recursão
	v := plain{Enabled: true}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*p = ProviderConfig(v)
	return nil
}

// Config é a configuração local persistida em config.json.
type Config struct {
	LogLevel  string                               `json:"logLevel"`
	Providers map[domain.ProviderID]ProviderConfig `json:"providers"`
	// Terminal onde as sessões são retomadas; vazio = detectar.
	Terminal terminal.Config `json:"terminal"`
	// Moeda que preenche o cadastro de preços (ISO 4217). Só sugestão: não
	// converte nem filtra valores.
	DefaultCurrency string `json:"defaultCurrency"`
}

// Default devolve a configuração inicial: todos os providers habilitados.
func Default() Config {
	c := Config{LogLevel: "info", Providers: map[domain.ProviderID]ProviderConfig{}, DefaultCurrency: "BRL"}
	for _, p := range domain.AllProviders() {
		c.Providers[p] = ProviderConfig{Enabled: true}
	}
	return c
}

// SlogLevel converte LogLevel; valor inválido já foi barrado por Validate.
func (c Config) SlogLevel() slog.Level {
	var l slog.Level
	if err := l.UnmarshalText([]byte(c.LogLevel)); err != nil {
		return slog.LevelInfo
	}
	return l
}

// Validate verifica valores que o usuário pode ter editado à mão.
func (c Config) Validate() error {
	var errs []error
	var l slog.Level
	if err := l.UnmarshalText([]byte(c.LogLevel)); err != nil {
		errs = append(errs, fmt.Errorf("logLevel %q inválido", c.LogLevel))
	}
	for id, pc := range c.Providers {
		if !id.Valid() {
			errs = append(errs, fmt.Errorf("provider %q desconhecido", id))
		}
		if pc.RootDir != "" && !filepath.IsAbs(pc.RootDir) {
			errs = append(errs, fmt.Errorf("%s.rootDir deve ser absoluto", id))
		}
		if pc.CLIPath != "" && !filepath.IsAbs(pc.CLIPath) {
			errs = append(errs, fmt.Errorf("%s.cliPath deve ser absoluto", id))
		}
	}
	if t := c.Terminal; t.Path != "" || len(t.Args) > 0 {
		if !filepath.IsAbs(t.Path) {
			errs = append(errs, errors.New("terminal.path deve ser absoluto"))
		}
		if err := terminal.CheckArgs(t.Args); err != nil {
			errs = append(errs, fmt.Errorf("terminal.args: %w", err))
		}
	}
	if !currencyRe.MatchString(c.DefaultCurrency) {
		errs = append(errs, fmt.Errorf("defaultCurrency %q deve ter 3 letras maiúsculas (ex.: BRL)", c.DefaultCurrency))
	}
	return errors.Join(errs...)
}

var currencyRe = regexp.MustCompile(`^[A-Z]{3}$`)

// Load lê o arquivo de configuração. Arquivo ausente devolve Default sem erro.
// Providers ausentes no arquivo recebem o valor padrão.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return Default(), fmt.Errorf("ler configuração: %w", err)
	}
	c := Default()
	if err := json.Unmarshal(data, &c); err != nil {
		return Default(), fmt.Errorf("configuração malformada: %w", err)
	}
	if c.Providers == nil { // "providers": null
		c.Providers = map[domain.ProviderID]ProviderConfig{}
	}
	for _, p := range domain.AllProviders() {
		if _, ok := c.Providers[p]; !ok {
			c.Providers[p] = ProviderConfig{Enabled: true}
		}
	}
	// Moeda só sugere o cadastro de preços: valor editado à mão é
	// normalizado e, se inválido, vira o padrão sem descartar o resto.
	c.DefaultCurrency = strings.ToUpper(strings.TrimSpace(c.DefaultCurrency))
	if !currencyRe.MatchString(c.DefaultCurrency) {
		c.DefaultCurrency = Default().DefaultCurrency
	}
	if err := c.Validate(); err != nil {
		return Default(), err
	}
	return c, nil
}

// Save grava de forma atômica (arquivo temporário + rename), permissão 0600.
func Save(path string, c Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".config-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op após rename bem-sucedido
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil { // conteúdo no disco antes do rename
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
