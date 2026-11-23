package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"sessionflow/internal/config"
	"sessionflow/internal/domain"
	"sessionflow/internal/host"
	"sessionflow/internal/service"
	"sessionflow/internal/terminal"
)

// check imprime um diagnóstico sem abrir a janela nem escrever nada: serve
// para suporte ("o que o SessionFlow enxerga aqui?") e para validar pacotes
// instalados. Não lê históricos, só verifica se os diretórios existem.
// Devolve o código de saída: 0, ou 1 se o básico (home, diretórios) falhar.
func check(w io.Writer) int {
	fmt.Fprintf(w, "SessionFlow %s (%s/%s)\n", version, runtime.GOOS, runtime.GOARCH)
	if host.Current.AppDir != "" {
		fmt.Fprintln(w, "Pacote: AppImage")
		if missing := host.Current.MissingWebKitTools(); len(missing) > 0 {
			fmt.Fprintln(w, "ERRO: faltam no sistema (pacotes bubblewrap e xdg-dbus-proxy):", strings.Join(missing, ", "))
		}
	} else {
		fmt.Fprintln(w, "Pacote: instalação nativa")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(w, "ERRO: diretório home:", err)
		return 1
	}
	paths, err := config.ResolvePaths(os.Getenv, home)
	if err != nil {
		fmt.Fprintln(w, "ERRO: diretórios do app:", err)
		return 1
	}
	cfg, cfgErr := config.Load(paths.ConfigFile())
	cfgState := "ok"
	switch {
	case cfgErr != nil:
		cfgState = "inválida, usando o padrão: " + cfgErr.Error()
	case !fileExists(paths.ConfigFile()):
		cfgState = "ausente (padrão; criada ao abrir o app)"
	}
	fmt.Fprintf(w, "Configuração: %s — %s\n", paths.ConfigFile(), cfgState)
	fmt.Fprintf(w, "Índice: %s — %s\n", paths.IndexDB(), sizeOf(paths.IndexDB()))
	fmt.Fprintf(w, "Log: %s\n", paths.LogFile())

	env := service.ProviderEnv{Config: cfg, Env: os.Getenv, Home: home, Paths: paths, LookPath: exec.LookPath}
	fmt.Fprintln(w, "Providers:")
	enabled := service.NewProviders(env)
	for _, id := range domain.AllProviders() {
		var line string
		pc := cfg.Providers[id]
		if !pc.Enabled {
			line = "desabilitado"
		}
		for _, p := range enabled {
			if p.ID() != id {
				continue
			}
			d := p.Detect(context.Background())
			cli := d.CLIPath
			if cli == "" {
				cli = "não encontrada"
			}
			line = fmt.Sprintf("%s · dados %s · CLI %s", d.Status, d.RootDir, cli)
			if d.Detail != "" {
				line += " · " + d.Detail
			}
		}
		fmt.Fprintf(w, "  %-13s %s\n", id.DisplayName(), line)
	}
	if t, err := terminal.Resolve(cfg.Terminal, exec.LookPath); err != nil {
		fmt.Fprintln(w, "Terminal: —", err)
	} else {
		fmt.Fprintln(w, "Terminal:", t.Path)
	}
	return 0
}

func sizeOf(p string) string {
	info, err := os.Stat(p)
	if err != nil {
		return "ainda não criado"
	}
	return fmt.Sprintf("%.1f MB", float64(info.Size())/(1<<20))
}

// testTerminal abre o terminal configurado (ou detectado) rodando args[1:]
// em args[0], pelo mesmo caminho da retomada (validação, argv sem shell). Diagnóstico de configuração do terminal.
func testTerminal(w io.Writer, args []string) int {
	if len(args) < 2 {
		fmt.Fprintln(w, "uso: sessionflow --test-terminal DIR CMD [ARGS...]")
		return 2
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(w, "ERRO:", err)
		return 1
	}
	paths, err := config.ResolvePaths(os.Getenv, home)
	if err != nil {
		fmt.Fprintln(w, "ERRO:", err)
		return 1
	}
	cfg, _ := config.Load(paths.ConfigFile())
	t, err := terminal.Resolve(cfg.Terminal, exec.LookPath)
	if err != nil {
		fmt.Fprintln(w, "ERRO:", err)
		return 1
	}
	dir, err := service.WorkDir(args[0])
	if err != nil {
		fmt.Fprintln(w, "ERRO:", err)
		return 1
	}
	cmd := terminal.Command{Path: args[1], Args: args[2:], Dir: dir}
	if err := (terminal.ExecLauncher{}).Launch(context.Background(), t, cmd); err != nil {
		fmt.Fprintln(w, "ERRO:", err)
		return 1
	}
	fmt.Fprintf(w, "aberto em %s: %q\n", t.Path, t.Argv(cmd))
	// O launcher recolhe o processo em segundo plano; espera o emulador
	// receber o comando antes de o processo sair.
	time.Sleep(2 * time.Second)
	return 0
}
