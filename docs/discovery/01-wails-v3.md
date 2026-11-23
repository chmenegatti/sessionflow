# 01 — Wails v3

> Fase 0 · Technical Discovery · 2026-11-21
>
> Legenda de origem: **[DOC]** documentação oficial · **[SRC]** código-fonte do módulo `github.com/wailsapp/wails/v3@v3.1.0` (baixado do proxy.golang.org e lido, sem instalar) · **[OBS]** observado nesta máquina · **[NÃO VERIFICADO]** não confirmado.

## 1. Status da versão

| Item | Valor | Origem |
|---|---|---|
| Última estável | `v3.1.0` (2026-11-01) | [SRC] `go list -m -versions` |
| Primeira estável | `v3.0.0` (2026-10-26), após `v3.0.0-beta.34` | [SRC] |
| Cadência | release semanal sobe minor; hotfix sobe patch; nightlies (`v3.2.0-nightly.*`) são pré-release e não resolvem em `@latest` | [SRC] `release_notes.md` |
| Runtime JS | `@wailsio/runtime` com **mesma versão** do módulo Go | [SRC] |
| Go mínimo | **Go ≥ 1.26** (`go 1.26.0`, `toolchain go1.26.10`); recomendado ≥ 1.26.10 por correção de `crypto/x509` | [SRC] `go.mod`, release notes |
| Go local | 1.26.1 — compila, abaixo do patch recomendado | [OBS] |
| Wails local | só `wails` v2.17.1 instalado; `wails3` ausente | [OBS] |

**Decisão:** v3 é estável, adotar **v3.1.0 com versão fixada** (Go e npm). Não misturar com v2 (CLI `wails` v2 instalada localmente coexiste; usar sempre `wails3`).

### Ressalvas conhecidas
- Advisory **GHSA-2p5f-2gx8-mh8m** parcialmente corrigido: no Linux, iframe *same-origin* ainda alcança a ponte nativa; chamadas de métodos vinculados a partir de iframe contornam a proteção. **Impacto no SessionFlow:** nunca renderizar conteúdo de sessão (HTML/markdown) em iframe nem como HTML cru — sanitizar markdown e desabilitar HTML embutido. [SRC/DOC]
- Backend Linux sem CGO (`-tags purego`) é experimental. [SRC]

## 2. Instalação

```bash
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.1.0
wails3 doctor
wails3 init -n sessionflow -t react     # "react" = React + TypeScript
```

- Templates em v3.1.0 [SRC `internal/templates/`]: `react` (TS), `react-js`, `vue`, `vue-js`, `svelte`, `svelte-js`, `vanilla`, `vanilla-js`, `ios`. **`react-ts` não existe** como nome embutido (existia em alphas antigas). `wails3 init -l` lista.
- `Taskfile.yml` na raiz inclui `build/<os>/Taskfile.yml`; `wails3 task <nome>` usa o Task embutido (não precisa do binário `task`). `wails3 dev`, `wails3 build`, `wails3 package` encapsulam tasks. [SRC]

## 3. Arquitetura relevante

| Conceito | API v3 | Origem |
|---|---|---|
| App | `application.New(application.Options{...})` | [SRC] |
| Serviços Go expostos ao front | `Services: []application.Service{application.NewService(&X{})}`; métodos exportados viram chamáveis | [SRC] |
| Ciclo de vida do serviço | `ServiceStartup(ctx, application.ServiceOptions) error` / `ServiceShutdown` (opcionais) | [SRC] `pkg/application/services.go` |
| Bindings TS | `wails3 generate bindings -ts` → `frontend/bindings/<module-path>/...`; rodado pelo Taskfile antes do build do front | [SRC] |
| Eventos (Go → front) | `app.Event.Emit("nome", dado)`; tipagem via `application.RegisterEvent[T]("nome")` em `init()` | [SRC] |
| Janelas | `app.Window.NewWithOptions(application.WebviewWindowOptions{...})` | [SRC] |
| Runtime front | `import { Events } from "@wailsio/runtime"`; `Events.On("nome", ev => ev.data)` | [SRC] template |

> Atenção: `app.EmitEvent(...)` é API de alphas antigas. Usar o estilo "manager" (`app.Event`, `app.Window`, `app.Menu`).

Exemplo mínimo (do template oficial, reduzido) [SRC `internal/templates/_common/main.go.tmpl`]:

```go
//go:embed all:frontend/dist
var assets embed.FS

func init() { application.RegisterEvent[string]("time") }

func main() {
    app := application.New(application.Options{
        Name:     "myapp",
        Services: []application.Service{application.NewService(&GreetService{})},
        Assets:   application.AssetOptions{Handler: application.AssetFileServerFS(assets)},
    })
    app.Window.NewWithOptions(application.WebviewWindowOptions{Title: "Window 1", URL: "/"})
    go func() { for { app.Event.Emit("time", time.Now().Format(time.RFC1123)); time.Sleep(time.Second) } }()
    if err := app.Run(); err != nil { log.Fatal(err) }
}
```

```ts
import { Events } from "@wailsio/runtime";
import { GreetService } from "../bindings/<module-path>";
GreetService.Greet(name).then(show);           // Promise
Events.On("time", (ev) => setTime(ev.data));   // payload em .data
```

### Mapeamento para o SessionFlow
- Cada serviço de aplicação (`SessionService`, `SearchService`, `ResumeService`, `SettingsService`) registrado como `application.Service`. Dependências injetadas no construtor (sem estado global).
- `ServiceStartup` dispara o scan inicial e o watcher; `ServiceShutdown` cancela o `context`.
- Eventos tipados: `sessions:changed`, `sync:progress`, `sync:error`, `provider:status`.

## 4. React + TypeScript + Vite

- Template `react`: React 18, Vite ^8.2, `@vitejs/plugin-react` ^6; `vite.config.ts` usa plugin `wails("./bindings")` de `@wailsio/runtime/plugins/vite`; dev server em `127.0.0.1:${WAILS_VITE_PORT:-9245}` com `strictPort`. [SRC]
- Tailwind: sem template oficial. Projeto Vite comum → `@tailwindcss/vite` deve funcionar. **[NÃO VERIFICADO]** — validar na Fase 1.
- shadcn/ui, Zustand, React Router: sem interação com Wails; dependências de front comuns. Validar na Fase 1.
- WebView Linux = WebKitGTK 6.0 → testar CSS moderno nele, não só no Chrome do dev server.

## 5. Dependências Linux

| Backend | Pacotes Ubuntu/Debian (dev) | Runtime | Status |
|---|---|---|---|
| **GTK4 + WebKitGTK 6.0 (padrão)** | `build-essential libgtk-4-dev libwebkitgtk-6.0-dev` | `libgtk-4-1 libwebkitgtk-6.0-4` | padrão desde alpha.93 [SRC/DOC] |
| GTK3 + webkit2gtk-4.1 | `libgtk-3-dev libwebkit2gtk-4.1-dev` + `-tags gtk3` | — | depreciado (doc fala em remoção) [DOC/SRC] |

- CGO obrigatório no backend padrão (gcc/clang). [SRC]
- NVIDIA: Wails define `WEBKIT_DISABLE_DMABUF_RENDERER=1` automaticamente se detectar `/sys/module/nvidia`. [SRC]

**Estado desta máquina [OBS]** (Ubuntu 26.04.1 LTS): gcc 15.2, `libgtk-4-dev` 4.22.4 instalado, `libwebkitgtk-6.0-4` (runtime) instalado, **`libwebkitgtk-6.0-dev` ausente** (candidato 2.52.6 disponível no apt). webkit2gtk-4.1 dev presente (2.52.6). → Pré-requisito da Fase 1: `sudo apt install libwebkitgtk-6.0-dev`.

## 6. Pendências para validar na Fase 1
1. `wails3 doctor` limpo após instalar `libwebkitgtk-6.0-dev`.
2. `wails3 init -t react` + build + execução de janela vazia.
3. Tailwind v4 + shadcn/ui no template.
4. Binding de um serviço com tipo de retorno struct e evento tipado.
5. Atualizar Go para ≥ 1.26.10.

## Fontes
- Módulo e versões: https://proxy.golang.org/github.com/wailsapp/wails/v3/@v/v3.1.0.zip
- Anúncio: https://wails.io/blog/wails-v3-released/
- Changelog: https://github.com/wailsapp/wails/blob/master/docs/mpress/content/changelog.md · https://v3.wails.io/fr/changelog
- Build Linux: https://wails.io/guides/build/linux/
- Migração v2→v3: https://wails.io/migration/v2-to-v3/
