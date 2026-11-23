# 06 — Linux: dependências e distribuição

> Fase 0 · 2026-11-21 · Legenda: **[DOC]** doc oficial · **[SRC]** código-fonte · **[OBS]** observado localmente · **[NÃO VERIFICADO]**

## 1. Dependências

### Build
| Componente | Pacote (Ubuntu/Debian) | Observação |
|---|---|---|
| Go ≥ 1.26 (rec. ≥ 1.26.10) | — | [SRC] Wails v3 `go.mod` |
| Compilador C | `build-essential` | CGO exigido pelo backend padrão [SRC] |
| GTK4 | `libgtk-4-dev` | [SRC] doctor apt list |
| WebKitGTK 6.0 | `libwebkitgtk-6.0-dev` | **ausente nesta máquina** [OBS] |
| Node + gerenciador | node 25 / pnpm 11 / npm 11 / bun 1.3 presentes | [OBS] |
| SQLite | nenhum pacote se usar `modernc.org/sqlite` | ver doc 05 |

### Runtime
`libgtk-4-1`, `libwebkitgtk-6.0-4`. As CLIs dos providers (`claude`, `codex`, `opencode`) são opcionais e externas.

## 2. Formatos de distribuição suportados pelo tooling Wails v3 [SRC]

| Formato | Task | Ferramenta |
|---|---|---|
| AppImage | `wails3 task linux:create:appimage` | `wails3 generate appimage` (linuxdeploy) |
| .deb | `wails3 task linux:create:deb` | nfpm (`build/linux/nfpm/nfpm.yaml`) |
| .rpm | `wails3 task linux:create:rpm` | nfpm |
| AUR | `wails3 task linux:create:aur` | — |
| Todos | `wails3 package GOOS=linux` | — |
| **Flatpak** | **sem suporte** no tooling nem na doc | — |

### AppImage — pontos de atenção
- Wails empacota explicitamente `WebKitWebProcess`, `WebKitNetworkProcess` e o injected bundle (`libwebkitgtkinjectedbundle.so` no GTK4). [SRC `internal/commands/appimage.go`]
- `AppRun` customizado deve manter cwd em `usr/lib` para o sandbox bubblewrap do WebKit. [DOC]
- Em sistemas com `.relr.dyn`, strip desabilitado → AppImage 20–40% maior. [DOC]
- AppImage não é sandbox: acesso livre a `~/.claude`, `~/.codex`, `~/.local/share/opencode` e às CLIs do host. **Caminho de menor atrito.**

### Flatpak — análise
- Sem manifesto oficial Wails; tentativas antigas (GNOME 3.38 + webkit2gtk-4.0) travaram (issue #3345, discussão #779). [DOC]
- v3 já tem código ciente de Flatpak (`internal/libpath/flatpak_linux.go`). `Options.Linux.ApplicationID` **precisa** ser definido em build sandbox, senão o web process do WebKit aborta (portal de nomes D-Bus). [SRC `application_options.go`]
- Runtime alvo: `org.gnome.Platform` ≥ 49 (GNOME 48 EOL em 2026-03-24). **[NÃO VERIFICADO]** qual versão do runtime traz `webkitgtk-6.0` — conferir com `flatpak run --command=pkg-config org.gnome.Sdk//49 --list-all | grep webkit`.
- `flatpak` 1.16.6 presente; `flatpak-builder` e `appimagetool` ausentes. [OBS]

finish-args necessários (sintaxe da doc Flatpak; **não testado**):

```
--share=ipc --socket=wayland --socket=fallback-x11 --device=dri
--filesystem=~/.claude:ro
--filesystem=~/.codex:ro
--filesystem=~/.local/share/opencode:ro
--talk-name=org.freedesktop.Flatpak    # flatpak-spawn --host para retomar sessões
```

**Conflitos com o produto:**
1. **Retomar sessão** exige executar `claude`/`codex`/`opencode` do host → `flatpak-spawn --host` → `--talk-name=org.freedesktop.Flatpak` = escape total do sandbox. Linter do Flathub sinaliza (`finish-args-flatpak-spawn-access`), exige aprovação manual; Flathub rejeita apps dependentes de componentes do host (exceções caso a caso) e considera ferramentas de desenvolvimento "geralmente pouco adequadas". [DOC]
2. **SQLite somente leitura (`:ro`) com WAL**: Codex e OpenCode usam bancos SQLite em modo WAL (doc 04/03). Ler um banco WAL exige acesso de escrita ao arquivo `-shm` (ou criá-lo). Montagem `:ro` pode impedir a leitura direta. Mitigação: copiar `*.db`, `*-wal` e `*-shm` para cache do app e ler a cópia — o que também garante "nunca modificar originais". **[NÃO VERIFICADO no sandbox]**
3. **CODEX_HOME / OPENCODE custom / CLAUDE_CONFIG_DIR**: diretórios personalizados fora dos grants exigem permissão extra (portal de arquivos ou `flatpak override`).

## 3. fsnotify / inotify

- Não recursivo: registrar watch por diretório e adicionar ao receber `Create` de diretório. [DOC fsnotify README]
- Limites: `fs.inotify.max_user_watches` (estouro → "no space left on device") e `max_user_instances` ("too many open files").
- Local [OBS]: `max_user_watches=65536`, `max_user_instances=128`; `~/.claude/projects` tem 262 diretórios — folga grande, mas IDEs consomem o mesmo orçamento.
- Estratégia: vigiar só diretórios necessários; erro de limite → fallback para varredura periódica (polling por mtime/tamanho).
- Flatpak: grants `--filesystem` são bind mounts, inotify deve funcionar. **[NÃO VERIFICADO]** Não depender de inotify em caminhos do document portal (`/run/user/$UID/doc`).

## 4. Recomendação

1. **MVP: AppImage + .deb** (tooling nativo do Wails, sem sandbox, retomada funciona).
2. **.rpm** quase grátis via nfpm.
3. **Flatpak: adiar para pós-MVP**, distribuído em repositório próprio (não Flathub) com `--talk-name=org.freedesktop.Flatpak` e leitura de SQLite via cópia. Registrar em ADR. Requer abstração `terminal.Launcher` com implementação `flatpak-spawn --host` — prevista desde a Fase 1, implementada só quando houver Flatpak.

## Fontes
- https://wails.io/guides/build/linux/
- https://github.com/wailsapp/wails/issues/3345 · https://github.com/wailsapp/wails/discussions/779
- https://docs.flatpak.org/en/latest/sandbox-permissions.html
- https://docs.flathub.org/docs/for-app-authors/requirements · https://docs.flathub.org/docs/for-app-authors/linter
- https://discourse.ubuntu.com/t/the-gnome-48-runtime-is-no-longer-supported-as-of-march-24-2026-ubuntu-24-04-4-lts-flatpak/80598
- https://github.com/fsnotify/fsnotify#readme
- https://www.sqlite.org/wal.html (requisito de `-shm` em leitura)
