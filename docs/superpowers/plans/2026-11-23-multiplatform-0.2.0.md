# SessionFlow 0.2.0 — Windows e macOS — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** O SessionFlow roda e é publicado no Windows 10/11 (amd64) e no macOS 13+ (universal), sem mudar o Linux (Etapa 1 do design, milestone 0.2.0, issues #1–#14).

**Architecture:** Toda diferença de sistema fica atrás de funções que recebem o GOOS (ou arquivos `_windows.go`/`_darwin.go`), testáveis em qualquer SO por tabela. O CI ganha runners Windows e macOS; o `release.yml` gera os pacotes de cada SO e um job `publish` junta tudo.

**Tech Stack:** Go 1.26, Wails v3.1.0 (`wails3` Taskfiles em `build/windows` e `build/darwin`), NSIS, `hdiutil`/`lipo`/`codesign`, GitHub Actions, `golang.org/x/sys/windows` (já indireta no `go.mod`).

**Spec:** `docs/superpowers/specs/2026-11-23-multiplatform-update-design.md` (Etapa 1 + "Decisões de entrega").

## Global Constraints

- Linux inalterado: mesmos caminhos XDG, pacotes e comportamento do 0.1.0; todos os testes atuais continuam passando.
- Sem shell em lugar nenhum (CLAUDE.md §10.8): nada de `cmd /c`, `powershell -c`, AppleScript, `.command` ou `sh -c`.
- Só `.exe` é aceito para retomar no Windows; shim `.cmd`/`.ps1` → plano "só copiar".
- Instalador Windows **por usuário**: `%LOCALAPPDATA%\Programs\SessionFlow`, chave em `HKCU`, sem UAC (`WAILS_INSTALL_SCOPE=user`).
- Portátil do Windows: dados em `%LOCALAPPDATA%` como o instalado.
- macOS: assinatura *ad-hoc* (`codesign --sign -`), sem notarização.
- Nomes dos assets: `SessionFlow-X.Y.Z-windows-amd64-setup.exe`, `SessionFlow-X.Y.Z-windows-amd64-portable.zip`, `SessionFlow-X.Y.Z-macos-universal.dmg`, `SessionFlow-X.Y.Z-macos-universal.zip`; Linux como hoje.
- Sem Flatpak. Sem dependência Go nova (só promover `golang.org/x/sys` a direta).
- Commits sem `Co-Authored-By` de IA; Conventional Commits; cada commit cita a issue (`(#N)`); fechar a issue quando a tarefa termina.
- `VERSION` só vira `0.2.0` na Task 14, depois do OK manual.

## Review Focus

1. **Home com espaço/acento no Windows** (`C:\Users\José Silva`): caminhos de dados, projeto e retomada funcionam → caso na tabela da Task 2 e na da Task 4.
2. **Diretório de projeto do Claude codificado no Windows** (`C--Users-jose-proj`): projeto aparece com o caminho real → fixture da Task 1, teste na Task 3.
3. **JSONL com CRLF** (Claude/Codex no Windows): mensagens lidas sem `\r` no texto → fixture e teste na Task 1.
4. **`opencode.db` aberto pelo OpenCode no Windows** (lock obrigatório de arquivo): leitura não falha nem trava o app; se não der para abrir, provider aparece com erro e os outros seguem → teste na Task 3 com o arquivo aberto em modo exclusivo (só no runner Windows, `//go:build windows`).
5. **Segunda instância no Windows**: não abre outra janela escrevendo no mesmo índice → teste da Task 6.

---

### Task 1: Discovery dos agentes no Windows e no macOS (#1)

**Files:**
- Modify: `docs/PROVIDERS.md` (seção "Windows e macOS")
- Create: `docs/discovery/fixtures/windows/claude/C--Users-jose-proj/<uuid>.jsonl` (CRLF, anonimizado), `docs/discovery/fixtures/windows/README.md`
- Test: `internal/provider/claude/claude_windows_fixture_test.go`, `internal/provider/codex/codex_crlf_test.go`

**Interfaces:**
- Produces (valores que as Tasks 3 e 4 usam, registrados em `docs/PROVIDERS.md`): pasta do OpenCode por SO; lista de caminhos candidatos de `claude`, `codex`, `opencode` por SO; regra de decodificação do nome de projeto do Claude no Windows.

- [ ] **Step 1:** Pesquisar na documentação/código atual de cada agente (OpenCode: uso de `xdg-basedir`; Claude Code: instalador nativo Windows/macOS e `npm`; Codex: instalador e `npm`) e registrar em `docs/PROVIDERS.md` os caminhos com a fonte de cada um. Valor de partida a confirmar: OpenCode = `~/.local/share/opencode` nos três SO.
- [ ] **Step 2: Testes que falham** — `TestClaudeWindowsProjectDir`: fixture `C--Users-jose-proj` → `ProjectPath == "C:\\Users\\jose\\proj"` quando lido com GOOS windows (função de decodificação recebe o GOOS); `TestCodexCRLF` e `TestClaudeCRLF`: JSONL com `\r\n` → nenhum texto de mensagem termina com `\r`.
- [ ] **Step 3:** Rodar `go test ./internal/provider/...` → FAIL nos novos.
- [ ] **Step 4:** Ajustar a decodificação do diretório de projeto do Claude para aceitar o GOOS (`decodeProjectDir(name, goos string) string`) e o leitor JSONL (`internal/provider/jsonl`) para remover `\r` final.
- [ ] **Step 5:** `go test ./internal/provider/...` → PASS. Commit `feat(providers): caminhos do Claude no Windows e JSONL com CRLF (#1)`; fechar #1.

### Task 2: Pastas da aplicação por sistema (#2)

**Files:**
- Modify: `internal/config/paths.go`, `app.go:46`, `check.go:40,110`
- Test: `internal/config/paths_test.go`

**Interfaces:**
- Produces: `func ResolvePaths(goos string, env Env, home string) (Paths, error)`; chamadores passam `runtime.GOOS`.

- [ ] **Step 1: Teste de tabela** `TestResolvePathsByOS`:
  - `linux` sem env → `~/.config/sessionflow`, `~/.local/share/sessionflow`, `~/.local/state/sessionflow`, `~/.cache/sessionflow` (atual); com `XDG_*` absolutos → respeitados.
  - `windows`, `APPDATA=C:\Users\José Silva\AppData\Roaming`, `LOCALAPPDATA=C:\Users\José Silva\AppData\Local` → Config `…\Roaming\SessionFlow`, Data `…\Local\SessionFlow\data`, State `…\Local\SessionFlow\logs`, Cache `…\Local\SessionFlow\cache`.
  - `windows` sem `APPDATA`/`LOCALAPPDATA` → `<home>\AppData\Roaming\SessionFlow` e `<home>\AppData\Local\SessionFlow\…`.
  - `darwin` → Config e Data `~/Library/Application Support/SessionFlow`, State `~/Library/Logs/SessionFlow`, Cache `~/Library/Caches/SessionFlow`; `XDG_*` ignorados.
  - home relativo → erro (atual).
  Use `filepath.Join` por GOOS: para testar Windows em Linux, montar com separador `\` explícito (helper `join(goos string, parts ...string) string`).
- [ ] **Step 2:** `go test ./internal/config/` → FAIL (assinatura).
- [ ] **Step 3:** Implementar; `IndexDB`/`UserDB` continuam em `DataDir` (no macOS Config e Data são a mesma pasta — `config.json` ao lado dos bancos é aceito).
- [ ] **Step 4:** `go test ./...` → PASS. Commit `feat(config): pastas da aplicação no Windows e no macOS (#2)`; fechar #2.

### Task 3: Raízes e executáveis dos providers por sistema (#3)

**Files:**
- Modify: `internal/config/paths.go` (`DefaultProviderRoot`), `internal/provider/util.go` (`FindCLI`, `isExecutable`), `internal/provider/{claude,codex,opencode}/*.go` (candidatos)
- Test: `internal/config/paths_test.go`, `internal/provider/util_test.go`, `internal/provider/opencode/opencode_windows_test.go`

**Interfaces:**
- Consumes: Task 1 (caminhos), Task 2 (`ResolvePaths(goos,…)`).
- Produces: `func DefaultProviderRoot(goos string, p domain.ProviderID, env Env, home string) string`; `func CLICandidates(goos string, p domain.ProviderID, env Env, home string) []string` (em `internal/provider`); `FindCLI` usa `exec.LookPath` (que já resolve `.exe` no Windows) e `isExecutable` por GOOS: no Windows = arquivo regular com extensão `.exe`.

- [ ] **Step 1: Testes** `TestDefaultProviderRootByOS` (os três agentes × três SO, com e sem `CLAUDE_CONFIG_DIR`/`CODEX_HOME`/`XDG_DATA_HOME`), `TestCLICandidatesByOS` (lista exata vinda da Task 1), `TestIsExecutableWindows` (`.exe` sim; `.cmd`, `.ps1`, sem extensão não). Review Focus 4: `TestOpenCodeLockedWindows` (`//go:build windows`): abrir `opencode.db` com `windows.CreateFile` sem `FILE_SHARE_READ`; `Discover` devolve erro do provider em < 5 s e não entra em pânico.
- [ ] **Step 2:** `go test ./internal/...` → FAIL.
- [ ] **Step 3:** Implementar; `isExecutable` vai para `util_unix.go`/`util_windows.go` se precisar de build tags.
- [ ] **Step 4:** `go test ./...` → PASS. Commit `feat(providers): pastas e CLIs dos agentes no Windows e no macOS (#3)`; fechar #3.

### Task 4: Retomada no Windows sem shell (#4)

**Files:**
- Modify: `internal/terminal/terminal.go` (`known`, `autoDetect` por GOOS), `internal/terminal/detach_other.go` → `detach_windows.go`, `internal/service/resume.go`, `frontend/src/features/sessions/*Resume*` (modo copiar)
- Test: `internal/terminal/terminal_test.go`, `internal/service/resume_test.go`

**Interfaces:**
- Produces: `func Resolve(goos string, c Config, look LookPath) (Terminal, error)`; entrada `"wt": {"-d", PlaceholderDir, "--", PlaceholderCmd}`; terminal sintético `Terminal{Path: "", Args: nil, Console: true}` = nova console via `CreateProcess` com `CREATE_NEW_CONSOLE` (campo novo `Console bool`); `ResumePlan` ganha `CopyOnly bool` e `CopyReason string`; `ResumeService.Resume` com `CopyOnly` devolve o plano sem lançar (`ErrCopyOnly`).

- [ ] **Step 1: Testes**
  - `TestResolveWindowsPrefersWT`: `look("wt")` ok → argv `wt.exe -d "C:\Users\José Silva\proj" -- C:\...\claude.exe --resume <id>` (cada item separado).
  - `TestResolveWindowsConsoleFallback`: sem `wt` → `Console: true`.
  - `TestResumeWindowsCmdShimIsCopyOnly`: CLI `claude.cmd` → `plan.CopyOnly == true`, `CopyReason` menciona "shell", launcher não chamado.
  - `TestResolveLinuxUnchanged`: `Resolve("linux", …)` igual ao atual.
- [ ] **Step 2:** `go test ./internal/terminal/ ./internal/service/` → FAIL.
- [ ] **Step 3:** Implementar; `ExecLauncher` com `Console` usa `SysProcAttr{CreationFlags: CREATE_NEW_CONSOLE}` em `launch_windows.go`; frontend: com `copyOnly`, o botão vira "Copiar comando" e mostra `copyReason`.
- [ ] **Step 4:** `go test ./...` e `cd frontend && npm test && npm run typecheck` → PASS. Commit `feat(resume): retomada no Windows sem shell (#4)`; fechar #4 depois do roteiro manual (Task 14) — até lá, comentar na issue "implementado, aguardando teste manual".

### Task 5: Retomada no macOS sem shell (#5)

**Files:**
- Modify: `internal/terminal/terminal.go`
- Test: `internal/terminal/terminal_test.go`, `internal/service/resume_test.go`

**Interfaces:**
- Consumes: Task 4 (`Resolve(goos,…)`, `CopyOnly`).
- Produces: no darwin, `autoDetect` procura (em ordem) `ghostty`, `kitty`, `wezterm`, `alacritty` no PATH e em `/Applications/<App>.app/Contents/MacOS/<bin>` (função `macAppCandidates(name string) []string`); sem nenhum → `ErrNoTerminal` vira `CopyOnly` com motivo "Terminal.app não aceita comando sem shell; instale Ghostty, kitty, WezTerm ou Alacritty, ou configure um terminal".

- [ ] **Step 1: Testes** `TestResolveDarwinFindsAppBundle` (look falha, arquivo existe em `/Applications/kitty.app/Contents/MacOS/kitty` simulado via `stat` injetado) → argv de kitty; `TestResumeDarwinNoTerminalIsCopyOnly`.
- [ ] **Step 2:** FAIL. **Step 3:** Implementar (injetar `stat func(string) bool` em `Resolve` via opção, sem tocar disco nos testes). **Step 4:** PASS. Commit `feat(resume): retomada no macOS sem shell (#5)`; comentar na issue como na Task 4.

### Task 6: Instância única e processos desacoplados no Windows (#6)

**Files:**
- Create: `internal/lockfile/lockfile_windows.go`; Modify: `internal/lockfile/lockfile_other.go` (build tag `!unix && !windows`), `go.mod` (`golang.org/x/sys` direta)
- Test: `internal/lockfile/lockfile_test.go`

**Interfaces:**
- Produces: `lock`/`unlock` com `windows.LockFileEx(h, LOCKFILE_EXCLUSIVE_LOCK|LOCKFILE_FAIL_IMMEDIATELY, …)`; `ERROR_LOCK_VIOLATION` → `ErrLocked`.

- [ ] **Step 1: Teste** `TestSecondAcquireFails` (já existe para unix; remover o skip/tag para rodar também no Windows): segundo `Acquire` no mesmo caminho → `ErrLocked`; após `Release`, `Acquire` ok.
- [ ] **Step 2:** No runner Windows (Task 8) ou `GOOS=windows go vet ./internal/lockfile/` local → falha de compilação/teste esperada antes da implementação.
- [ ] **Step 3:** Implementar. **Step 4:** `GOOS=windows go build ./...` e `GOOS=darwin go build ./...` locais passam. Commit `feat(lockfile): instância única no Windows (#6)`; fechar #6 quando o CI Windows (Task 8) estiver verde.

### Task 7: Watcher no macOS (#7)

**Files:**
- Create: `internal/watcher/rlimit_darwin.go`, `internal/watcher/rlimit_other.go`
- Modify: `internal/watcher/watcher.go` (chamar `raiseFileLimit()` em `Start`), `docs/ARCHITECTURE.md` (seção do watcher)
- Test: `internal/watcher/watcher_test.go`

**Interfaces:**
- Produces: `func raiseFileLimit() (soft uint64)` — no darwin sobe o soft `RLIMIT_NOFILE` para `min(hard, 10240)` e devolve o valor; nos outros, no-op. Estouro continua caindo em `Degraded()` + reconciliação (comportamento atual do limite do inotify).

- [ ] **Step 1: Teste** `TestDegradedWhenAddFails` (já cobre? se não, injetar `fs.Add` que devolve `syscall.EMFILE` → `Degraded() == true`, sem pânico, sem novas tentativas no mesmo dir).
- [ ] **Step 2–4:** FAIL → implementar → PASS. A medição real (quantos descritores com o `~/.claude/projects` do mantenedor) vai para o roteiro da Task 14. Commit `feat(watcher): limite de arquivos no macOS (#7)`.

### Task 8: CI em Linux, Windows e macOS (#8)

**Files:**
- Modify: `.github/workflows/ci.yml`

**Interfaces:**
- Produces: jobs `linux` (o atual, inalterado), `windows` (`windows-latest`) e `macos` (`macos-latest`): checkout, Go, Node 22, `wails3@v3.1.0`, bindings, `npm ci && npm run build`, `go vet ./...`, `go test ./...` (sem `-race` fora do Linux), `wails3 build` do SO.

- [ ] **Step 1:** Escrever os jobs; abrir PR (branch `ci/multiplataforma`).
- [ ] **Step 2:** Esperado: os três verdes no PR. Falha de teste específica de SO → corrigir na task dona do código (não pular teste com `t.Skip` por SO sem motivo escrito no teste).
- [ ] **Step 3:** Merge. Commit `ci: testes e build no Windows e no macOS (#8)`; fechar #8.

### Task 9: Autoteste sem janela (#9)

**Files:**
- Create: `selftest.go` (raiz, `package main`)
- Modify: `main.go` (flag `--selftest`, ajuda)
- Test: `selftest_test.go`

**Interfaces:**
- Produces: `func selftest(w io.Writer, fixtures string) int` — cria diretório temporário, resolve `Paths` com `home` temporário, abre `index.db`/`user.db` (migrations), indexa os fixtures de `fixtures` (por padrão os embutidos `docs/discovery/fixtures/usage` via `embed`), confere ≥ 1 sessão de cada agente, fecha, apaga o temporário; imprime `SELFTEST OK` e devolve 0, ou a etapa que falhou e 1. Não lê o home real.

- [ ] **Step 1: Teste** `TestSelftestOK` (saída contém `SELFTEST OK`, código 0, nenhum arquivo criado fora de `t.TempDir()`) e `TestSelftestReportsFailure` (fixtures inexistentes → código 1, mensagem com a etapa).
- [ ] **Step 2–4:** FAIL → implementar → PASS. Adicionar `./bin/sessionflow --selftest` como passo nos três jobs do CI. Commit `feat: --selftest sem janela (#9)`; fechar #9.

### Task 10: Pacotes do Windows (#10)

**Files:**
- Modify: `build/windows/nsis/project.nsi` (`!define WAILS_INSTALL_SCOPE "user"`; ao fim de instalação silenciosa, relançar o app se `/RELAUNCH` foi passado), `build/config.yml` (nome/versão), `.github/workflows/release.yml` (job `windows`)
- Create: `packaging/scripts/windows-package.sh` (bash do runner: build, NSIS, zip)

**Interfaces:**
- Produces: artefato `dist-windows` com `SessionFlow-$V-windows-amd64-setup.exe` e `SessionFlow-$V-windows-amd64-portable.zip` (contém só `SessionFlow.exe` + `LICENSE`).

- [ ] **Step 1:** Script e job; disparar o workflow manualmente (`workflow_dispatch`) num branch.
- [ ] **Step 2:** Esperado no job: setup instala com `/S` em `%LOCALAPPDATA%\Programs\SessionFlow` (verificar com `test -f`), `SessionFlow.exe --version` e `--selftest` passam no instalado e no extraído do zip, desinstalador `/S` remove a pasta e a chave HKCU.
- [ ] **Step 3:** Commit `build(windows): instalador por usuário e zip portátil (#10)`; fechar #10.

### Task 11: Pacotes do macOS (#11)

**Files:**
- Modify: `build/darwin/Info.plist` (versão, `LSMinimumSystemVersion` = `13.0`), `.github/workflows/release.yml` (job `macos`)
- Create: `packaging/scripts/macos-package.sh`

**Interfaces:**
- Produces: artefato `dist-macos` com `SessionFlow-$V-macos-universal.dmg` e `SessionFlow-$V-macos-universal.zip` (`ditto -c -k --keepParent SessionFlow.app`).

- [ ] **Step 1:** Build arm64 e amd64 (`wails3 task darwin:build:universal` ou build por arquitetura + `lipo -create`), `codesign --force --deep --sign -`, `.dmg` com `hdiutil create -format UDZO` (link para `/Applications`), `.zip` com `ditto`.
- [ ] **Step 2:** Esperado no job: `lipo -info` lista `x86_64 arm64`; `codesign --verify --deep --strict` ok; `SessionFlow.app/Contents/MacOS/SessionFlow --version` e `--selftest` passam.
- [ ] **Step 3:** Commit `build(macos): dmg universal e zip do app (#11)`; fechar #11.

### Task 12: Release unificado (#12)

**Files:**
- Modify: `.github/workflows/release.yml`, `packaging/scripts/release-notes.sh` (se precisar)

**Interfaces:**
- Consumes: artefatos `dist` (Linux atual), `dist-windows`, `dist-macos`.
- Produces: `publish` (needs: linux, windows, macos) baixa os três, gera `SHA256SUMS` de todos os pacotes, cria o release com as notas do CHANGELOG. Tag com sufixo (`v0.2.0-rc.1`) cria **pre-release** (`--prerelease`) para teste.

- [ ] **Step 1:** Ajustar; a checagem `test "v$(cat VERSION)" = tag` aceita sufixo `-rc.N` quando `VERSION` é a base.
- [ ] **Step 2:** Tag `v0.2.0-rc.1` (só depois das Tasks 1–11 no `main`) → pre-release com 4 pacotes Linux + 2 Windows + 2 macOS + `SHA256SUMS`; `sha256sum -c` local ok.
- [ ] **Step 3:** Commit `ci: release com Windows e macOS (#12)`; fechar #12.

### Task 13: Documentação da 0.2.0 (#13)

**Files:**
- Modify: `README.md` (instalação por SO; SmartScreen "Mais informações → Executar assim mesmo"; macOS "Abrir" pelo menu de contexto ou `xattr -dr com.apple.quarantine /Applications/SessionFlow.app`), `docs/INSTALL.md`, `docs/PROVIDERS.md`, `docs/ARCHITECTURE.md`, `docs/ROADMAP.md` (remove "só Linux" das limitações), `CHANGELOG.md` (`[Não lançado]`)
- Create: `docs/adr/0002-multiplataforma.md` (decisões: por usuário, ad-hoc, só `.exe`, Terminal.app copiar, sem Flatpak)

- [ ] **Step 1:** Escrever. **Step 2:** Conferir links relativos (`grep -o '](docs/[^)]*)' README.md` → arquivos existem). Commit `docs: instalação e arquitetura no Windows e no macOS (#13)`; fechar #13.

### Task 14: Roteiro manual e publicação da 0.2.0 (#14)

**Files:**
- Create: `docs/manual-tests/0.2.0.md`
- Modify: `VERSION` (`0.2.0`), `CHANGELOG.md` (`[0.2.0] - <data>`)

- [ ] **Step 1:** Roteiro com caixas para Windows 10/11 e macOS (Intel e Apple Silicon, se houver): baixar do pre-release; SmartScreen/Gatekeeper; instalar; abrir; providers detectados; indexar; buscar; abrir conversa; retomar (com `wt`, sem `wt`, com shim `.cmd`; macOS com e sem emulador); analytics/custos; segunda instância; desinstalar; portátil; **macOS: contar descritores** (`lsof -p <pid> | wc -l`) com o histórico real e registrar se o watcher ficou degradado.
- [ ] **Step 2:** Pedir ao mantenedor para executar e registrar o resultado na issue #14. **Não seguir sem o OK.**
- [ ] **Step 3:** Com o OK: `VERSION` = `0.2.0`, CHANGELOG datado, commit `chore(release): v0.2.0`, tag `v0.2.0`, acompanhar o workflow, conferir assets e `SHA256SUMS`; fechar #4, #5, #7 e #14 e o milestone.
