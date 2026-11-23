package main

import (
	"embed"
	"strings"

	"errors"
	"fmt"
	"os"
	"sessionflow/internal/host"
)

// Frontend compilado (frontend/dist) embutido no binário.
//
//go:embed all:frontend/dist
var assets embed.FS

// version é sobrescrita no build de release via -ldflags "-X main.version=…".
var version = "0.1.0-dev"

const usage = `Uso: sessionflow [opção]

Sem opção, abre o SessionFlow.

  --check     diagnóstico (caminhos, providers, CLIs, terminal) sem abrir a janela
  --test-terminal DIR CMD [ARGS...]
              abre o terminal configurado rodando CMD em DIR, como a retomada faz
  --version   mostra a versão
  --help      mostra esta ajuda
`

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--version", "-v":
			fmt.Println("SessionFlow", version)
			return
		case "--check":
			os.Exit(check(os.Stdout))
		case "--test-terminal":
			os.Exit(testTerminal(os.Stdout, os.Args[2:]))
		case "--help", "-h":
			fmt.Print(usage)
			return
		}
	}
	// AppImage num sistema sem as ferramentas de sandbox do WebKit: o WebKit
	// abortaria o processo sem explicação ao abrir a janela.
	if missing := host.Current.MissingWebKitTools(); len(missing) > 0 {
		fmt.Fprintf(os.Stderr, "sessionflow: o WebKitGTK precisa de %s no sistema.\n"+
			"Instale os pacotes bubblewrap e xdg-dbus-proxy (Debian/Ubuntu: sudo apt install bubblewrap xdg-dbus-proxy;\n"+
			"Fedora: sudo dnf install bubblewrap xdg-dbus-proxy) ou use o .deb ou o .rpm.\n", strings.Join(missing, " e "))
		os.Exit(1)
	}
	err := run()
	switch {
	case err == nil:
	case errors.Is(err, errAlreadyRunning):
		fmt.Fprintln(os.Stderr, "sessionflow:", err)
	default:
		fmt.Fprintln(os.Stderr, "sessionflow:", err)
		showStartupError(err)
		os.Exit(1)
	}
}
