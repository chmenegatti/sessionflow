# Instalação

Windows 10/11 x64, macOS 13+ (universal) e Linux x86_64. Os arquivos ficam no [GitHub Releases](https://github.com/chmenegatti/sessionflow/releases) (veja "Publicação" no fim).

## Windows

| Arquivo | O que faz |
|---|---|
| `SessionFlow-X.Y.Z-windows-amd64-setup.exe` | instala para o seu usuário em `%LOCALAPPDATA%\Programs\SessionFlow`, sem pedir administrador; atalho no Menu Iniciar e na área de trabalho; desinstala por *Configurações → Aplicativos* |
| `SessionFlow-X.Y.Z-windows-amd64-portable.zip` | só o `sessionflow.exe`: extraia e rode; os dados ficam nas mesmas pastas da versão instalada |

- Requer o WebView2 (já vem no Windows 11 e no 10 atualizado; o instalador baixa se faltar).
- **SmartScreen:** o instalador não tem assinatura paga. Na primeira execução, *Mais informações* → *Executar assim mesmo*.
- **Retomar sessões:** abre no Windows Terminal (`wt.exe`) ou, sem ele, numa janela de console. CLI instalada pelo npm (`claude.cmd`, `codex.cmd`) só roda por um shell, que o SessionFlow não usa: o botão vira *Copiar comando*.
- Todos os pacotes são verificados no CI: instalação silenciosa, `--version`, `--selftest` e desinstalação.

## macOS

`SessionFlow-X.Y.Z-macos-universal.dmg`: abra e arraste o SessionFlow para *Aplicativos*. Funciona em Intel e Apple Silicon, macOS 13 ou mais novo.

- **Gatekeeper:** o app tem assinatura *ad-hoc* (sem conta Apple Developer). Na primeira abertura, clique com o botão direito no app → *Abrir* → *Abrir*, ou rode `xattr -dr com.apple.quarantine /Applications/SessionFlow.app`.
- **Retomar sessões:** abre no kitty, WezTerm, Alacritty ou Ghostty (no PATH ou em `/Applications`). Só com o Terminal.app, o botão vira *Copiar comando*: abrir um comando no Terminal.app exigiria AppleScript ou shell.
- Verificado no CI: binário universal (`lipo`), `codesign --verify`, `--version` e `--selftest`.

## Linux

Todos os pacotes saem do mesmo binário, compilado em Ubuntu 24.04.

| Formato | Para quem | Requisitos | Validado em contêiner limpo (instalar, abrir, usar, remover) |
|---|---|---|---|
| `.deb` | Ubuntu 24.04+, Debian 13+ | o apt instala GTK4 e WebKitGTK 6.0 | ✔ Ubuntu 24.04.5 · ✔ Debian 13 |
| `.rpm` | Fedora 39+ | o dnf instala GTK4 e WebKitGTK 6.0 | ✔ Fedora 45 |
| AppImage | distros com glibc ≥ 2.38 | desktop gráfico, mais `bubblewrap` e `xdg-dbus-proxy` (já presentes onde há WebKitGTK ou Flatpak); traz GTK4 e WebKitGTK | ✔ Debian 13 e ✔ Fedora 45, ambos sem GTK4/WebKitGTK no sistema · ✘ Ubuntu 22.04 (glibc 2.35, sem suporte) |
| `.tar.gz` | instalação manual | `libgtk-4-1` e `libwebkitgtk-6.0-4` | ✔ Ubuntu 24.04.5 |

Cada validação, em contêiner novo e como usuário comum, com históricos de teste dos três agentes:
- instala o pacote e confere `.desktop`, ícones e metainfo;
- roda `--version` e `--check`;
- abre a janela num X virtual por 25 s e captura a tela;
- confere a indexação e a ausência de erros no log e no stderr;
- abre uma segunda instância, que sai e mantém a primeira;
- desinstala e confere que dados e históricos ficaram.

As evidências (logs, capturas, resultados) ficam em `dist/test/<alvo>/`, geradas por `make test-install`.

## Conferir os arquivos

```bash
sha256sum -c SHA256SUMS --ignore-missing
```

## .deb (Ubuntu, Debian)

```bash
sudo apt install ./sessionflow_0.1.0_amd64.deb
sessionflow --version
```

Instala `/usr/bin/sessionflow`, o atalho no menu (SessionFlow) e os ícones. Para remover: `sudo apt remove sessionflow`. Seus dados ficam; veja "Dados".

## .rpm (Fedora)

```bash
sudo dnf install ./sessionflow-0.1.0-1.x86_64.rpm
```

Para remover: `sudo dnf remove sessionflow`.

## AppImage

```bash
chmod +x SessionFlow-0.1.0-x86_64.AppImage
./SessionFlow-0.1.0-x86_64.AppImage
```

- **Requisitos medidos:**
  - glibc ≥ 2.38: as bibliotecas embutidas vêm do Ubuntu 24.04, e o Ubuntu 22.04 (glibc 2.35) não roda.
  - `bubblewrap` e `xdg-dbus-proxy` em `/usr/bin`: o WebKit isola o processo web com eles, em caminhos fixos, e por isso eles não vão dentro da imagem. Sem eles, a AppImage sai com a instrução de instalação: `sudo apt install bubblewrap xdg-dbus-proxy` ou `sudo dnf install bubblewrap xdg-dbus-proxy`.
  - Bibliotecas básicas de qualquer desktop, que o linuxdeploy de propósito não embute: OpenGL/EGL/GLES (Mesa), harfbuzz, fribidi, fontconfig, freetype, X11/xkbcommon/Wayland, libgpg-error e libcom_err.
- A AppImage usa FUSE 2. Se não abrir com "dlopen(): error loading libfuse.so.2", instale `libfuse2t64` (Ubuntu 24.04+) ou `fuse-libs` (Fedora). Outra opção é rodar com `--appimage-extract-and-run`.
- Para ter o atalho no menu, use um integrador (Gear Lever, AppImageLauncher) ou copie o `.desktop` e o ícone do `.tar.gz` para `~/.local/share/applications` e `~/.local/share/icons/hicolor`.

## .tar.gz

```bash
sudo apt install libgtk-4-1 libwebkitgtk-6.0-4      # Fedora: sudo dnf install gtk4 webkitgtk6.0
mkdir -p ~/.local/opt/sessionflow && tar -xzf sessionflow-0.1.0-linux-amd64.tar.gz -C ~/.local/opt/sessionflow
~/.local/opt/sessionflow/sessionflow
```

## Primeira execução

```bash
sessionflow --check
```

Mostra, sem abrir a janela e sem escrever nada, o que o SessionFlow encontra:
- versão e tipo de pacote;
- configuração, índice e log;
- cada provider (dados e CLI);
- o terminal usado para retomar.

Ao abrir o app, a primeira indexação roda em segundo plano (cerca de 20 s para 2 GB de históricos).

Para testar a retomada sem abrir uma sessão:

```bash
sessionflow --test-terminal ~/algum/projeto /usr/bin/env
```

Abre o terminal configurado rodando o comando no diretório, do mesmo jeito que a retomada faz.

## Dados

| O quê | Linux | Windows | macOS |
|---|---|---|---|
| Configuração | `~/.config/sessionflow/config.json` | `%APPDATA%\SessionFlow\config.json` | `~/Library/Application Support/SessionFlow/config.json` |
| Índice (reconstruível) | `~/.local/share/sessionflow/index.db` | `%LOCALAPPDATA%\SessionFlow\data\index.db` | `~/Library/Application Support/SessionFlow/index.db` |
| Favoritos, tags, preços | `~/.local/share/sessionflow/user.db` | `%LOCALAPPDATA%\SessionFlow\data\user.db` | `~/Library/Application Support/SessionFlow/user.db` |
| Log | `~/.local/state/sessionflow/sessionflow.log` | `%LOCALAPPDATA%\SessionFlow\logs\sessionflow.log` | `~/Library/Logs/SessionFlow/sessionflow.log` |

Desinstalar o pacote não apaga esses arquivos nem, em hipótese alguma, os históricos dos agentes.

## Problemas comuns

| Sintoma | O que fazer |
|---|---|
| Janela com erro "O SessionFlow não conseguiu iniciar" | A mensagem diz o arquivo e o motivo (permissão, disco cheio). O caminho do log aparece na tela. |
| Abrir de novo não abre outra janela | É o esperado: a janela existente vem para a frente. |
| "nenhum emulador de terminal encontrado" | Configurações → Terminal: informe o caminho e, para emuladores desconhecidos, os argumentos com `{dir}` e `{cmd}`. |
| Retomar recusa caminho com `$` | O ptyxis altera argumentos com `$`; escolha outro terminal em Configurações. |
| Alterações nos históricos demoram a aparecer | O limite de inotify pode ter sido atingido (`fs.inotify.max_user_watches`). O app cai para uma verificação por minuto; aumentar o limite resolve. |
| Tela branca no WebKit em GPUs/drivers antigos | `WEBKIT_DISABLE_DMABUF_RENDERER=1 sessionflow` |

## Compilar e empacotar

Requer Docker. As versões de Go, Node e Wails ficam fixas em `packaging/docker/Dockerfile.build`.

```bash
make release        # dist/: AppImage, .deb, .rpm, .tar.gz, SHA256SUMS (build em Ubuntu 24.04)
make test-install   # instala e executa cada pacote em contêineres limpos; evidências em dist/test/
```

## Publicação (GitHub Releases)

1. Atualize `VERSION` e o `CHANGELOG.md`.
2. Crie a tag: `git tag v$(cat VERSION) && git push --tags`.

O workflow `.github/workflows/release.yml` confere a tag com `VERSION` e então:
- builda e roda os testes de instalação;
- publica o release com `SHA256SUMS` e as notas do CHANGELOG.

Para publicar à mão a partir de `dist/`:

```bash
packaging/scripts/release-notes.sh "$(cat VERSION)" > /tmp/notes.md
gh release create "v$(cat VERSION)" dist/*.AppImage dist/*.deb dist/*.rpm dist/*.tar.gz dist/SHA256SUMS \
  --title "SessionFlow $(cat VERSION)" --notes-file /tmp/notes.md
```
