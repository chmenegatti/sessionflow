# ADR 0009 — Empacotamento de release

**Status:** aceito · 2026-11-22 (Fase 7). Mantém a decisão do ADR 0004: formatos comuns, sem Flatpak.

## Contexto
O MVP sai para Linux. O binário depende de GTK4 e WebKitGTK 6.0 (CGO) e da glibc da máquina onde foi compilado. Ele precisa ler os diretórios dos agentes e executar as CLIs deles.

## Decisão
- **Build único em contêiner Ubuntu 24.04** (`packaging/docker/Dockerfile.build`, versões fixas de Go, Node e Wails). O binário em si exige só GLIBC_2.34. O mesmo binário vai para AppImage, .deb, .rpm e tar.gz.
- **AppImage** pela ferramenta do Wails (`wails3 generate appimage`), que leva GTK4, WebKitGTK e os processos auxiliares do WebKit para dentro da imagem. O pacote `internal/host` tira do ambiente dos processos filhos (terminal, CLI) o que o AppRun injeta. Também confere `bubblewrap` e `xdg-dbus-proxy`, que o WebKit procura em caminhos fixos e não vão na imagem.
- **.deb/.rpm** com nfpm (`wails3 tool package`), em `/usr/bin`, com `.desktop`, ícones `hicolor` e AppStream metainfo. As dependências são resolvidas pelo gerenciador da distro.
- **Testes de instalação** em contêineres limpos (`make test-install`) fazem parte do release: instalar, `--version`, `--check`, GUI num X virtual com captura de tela, segunda instância e desinstalação.
- **Flatpak descartado** por decisão do usuário. O manifesto chegou a ser feito e testado, e o registro fica aqui:
  - o app precisa de `host:ro` e `org.freedesktop.Flatpak` (`flatpak-spawn --host`), então o sandbox não isola nada;
  - no runtime GNOME 51 o `WebKitWebProcess` entra em laço (100% de CPU) e a janela fica em branco com o Flatpak 1.16.6; um WebView mínimo, sem o SessionFlow, reproduz;
  - no GNOME 50 funcionou.

## Consequências
- A AppImage exige glibc ≥ 2.38 (as bibliotecas embutidas vêm do Ubuntu 24.04; o Ubuntu 22.04 não roda) e `bubblewrap` e `xdg-dbus-proxy` no sistema. Sem eles, o app sai com a instrução de instalação em vez de abortar.
- .deb, .rpm e tar.gz dependem do GTK4 e do WebKitGTK 6.0 da distro: Ubuntu 24.04+, Debian 13+, Fedora 39+.
- Medições em `docs/INSTALL.md`.
