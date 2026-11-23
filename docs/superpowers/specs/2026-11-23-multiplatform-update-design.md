# SessionFlow multiplataforma e atualização autorizada — design

Data: 2026-11-23. Estado: aguardando aprovação. Revisado por um leitor independente (perguntas respondidas na seção "Decisões de entrega").

## Intenção

SessionFlow 0.1.0 é público (github.com/chmenegatti/sessionflow), mas só roda e só é distribuído no Linux, e versão nova só chega por download manual. O usuário quer:

1. **Distribuição para o público geral** em Windows, macOS e Linux, nos formatos que o Zeno já publica (instalador e .zip no Windows, .dmg no macOS, .deb/.rpm/AppImage/tar.gz no Linux). **Sem Flatpak.**
2. **Atualização dentro do app**: o app percebe versão nova, **avisa**, e **só instala com autorização** do usuário.

Referência existente: o Zeno (`ideias/zeno`, Wails v2) já publica nos três sistemas sem certificado pago (macOS *ad-hoc*, Windows sem assinatura) e tem aviso de versão (`wails/update.go`), mas só abre a página do release.

## Objetivos

- **Etapa 1 — Portar e distribuir.** O app funciona no Windows 10/11 (amd64) e no macOS 13+ (Intel e Apple Silicon): descobre, indexa, busca, mostra, retoma sessões e faz analytics como no Linux. O release da tag publica, além do que já existe:
  - Windows: `SessionFlow-X.Y.Z-windows-amd64-setup.exe` (NSIS) e `SessionFlow-X.Y.Z-windows-amd64-portable.zip`;
  - macOS: `SessionFlow-X.Y.Z-macos-universal.dmg` e `SessionFlow-X.Y.Z-macos-universal.zip` (o `.app` zipado, usado pela atualização);
  - Linux: AppImage, .deb, .rpm, tar.gz (como hoje).
  - `SHA256SUMS` cobrindo todos, mais `SHA256SUMS.sig` (Etapa 2).
- **Etapa 2 — Atualização autorizada.** O app consulta a última release; havendo versão nova, mostra aviso com a versão e as notas; com "Atualizar", baixa o pacote do formato instalado, confere integridade e assinatura, instala e reinicia (ou instrui quando não dá).

## Decisões de entrega (respostas do usuário e chamadas do design)

- **Versões:** Etapa 1 sai como **0.2.0** (Windows e macOS, sem atualizador). Etapa 2 sai como **0.3.0** (atualização autorizada + `SHA256SUMS.sig`). *(usuário)*
- **Chave de assinatura:** durante o desenvolvimento, chave de **teste** (só em testes, nunca embutida em build de release). Antes da tag 0.3.0 o usuário gera o par com `go run ./cmd/sign keygen`, cadastra a privada com `gh secret set SESSIONFLOW_SIGNING_KEY` e a pública entra em `internal/update/pubkey.go` num commit. Passo a passo em `docs/RELEASING.md`. O `publish` só exige o segredo a partir da 0.3.0 (a 0.2.0 não assina). *(usuário)*
- **Validação manual:** o usuário tem máquinas **Windows e macOS**. Cada etapa termina com um roteiro manual (`docs/manual-tests/`) nos dois sistemas: interface, retomada, SmartScreen/Gatekeeper, instalador e, na Etapa 2, cada formato de atualização. **Nada é publicado antes do OK do usuário nesse roteiro.** A medição do watcher no macOS com o volume real de `~/.claude/projects` entra no roteiro. *(usuário)*
- **Instalador do Windows por usuário:** NSIS instala em `%LOCALAPPDATA%\Programs\SessionFlow`, desinstalação em `HKCU`, sem UAC; a atualização `/S` roda sem pedir elevação. Detecção de `windows-setup`: executável sob `%LOCALAPPDATA%\Programs\SessionFlow` ou chave `HKCU\…\Uninstall\SessionFlow`. *(chamada do design: padrão de apps modernos, ex. VS Code user installer; evita UAC e dono errado de `%APPDATA%`)*
- **Portátil do Windows:** "portátil" = sem instalador; dados continuam em `%LOCALAPPDATA%` (como o Zeno). Atualizar o portátil troca só os arquivos do programa. *(chamada do design)*

## Não-objetivos

- Flatpak, Snap, Microsoft Store, Mac App Store, Homebrew, winget, Scoop.
- Certificado pago (Apple Developer ID/notarização, Authenticode). Fica como pendência documentada.
- Windows ARM64, Linux ARM64, Windows 7/8, macOS < 13.
- Atualização silenciosa ou forçada; canal beta; rollback automático para a versão anterior; delta updates.
- Atualizar sem rede pela própria interface (o usuário pode sempre instalar o pacote manualmente).

## Anti-objetivos (falha mesmo que "funcione")

- Qualquer requisição de rede sem o usuário ter permitido verificar atualizações (regra do projeto: offline e sem telemetria por padrão).
- Enviar algo além de um GET anônimo à API de releases (sem ID de instalação, sem versão em query, User-Agent genérico `SessionFlow`).
- Instalar um arquivo cuja assinatura ed25519 do `SHA256SUMS` não confira ou cujo hash não esteja nele.
- Executar o instalador/pacote baixado sem o usuário clicar em "Atualizar" naquela notificação.
- Passar por shell para instalar (regra 8): `pkexec`, `msiexec`/setup e `ditto` recebem argv separado.
- Regredir o Linux: tudo que o 0.1.0 faz continua funcionando igual.
- Tocar nos históricos originais dos agentes (regras 1–2) em qualquer plataforma.

## Restrições

- Go + Wails **v3** (não misturar com APIs v2 do Zeno). Build de Windows e macOS precisa rodar no respectivo runner do GitHub Actions (CGO/WebView2/WKWebView). Usar a toolchain/Taskfile do Wails v3 já em `build/windows` e `build/darwin`.
- Sem dependência Go nova sem justificativa: assinatura com `crypto/ed25519` da stdlib; download com `net/http`; zip com `archive/zip`.
- Segurança do CLAUDE.md §10 continua valendo em todos os sistemas (sem shell, validar executáveis e diretórios, nunca ler credenciais dos providers).
- Versão = arquivo `VERSION`; tag `vX.Y.Z` igual (workflow atual).

## Abordagens consideradas (atualização)

| | O que faz | Por que sim/não |
|---|---|---|
| A. Só aviso (modelo Zeno) | banner + abre a página do release | pequeno, mas não atende "atualizar com autorização" |
| **B. Aviso + instalação autorizada por formato (escolhida)** | baixa o asset do formato instalado, verifica ed25519 + SHA-256, instala | atende o pedido; custo médio-grande; uma lógica por formato |
| C. Biblioteca de self-update (go-selfupdate etc.) | troca só o executável | não cobre setup.exe, .app, .deb/.rpm; dependência extra |

Escolha do usuário: **B**, com a Etapa 1 antes (os formatos determinam os instaladores).

## Etapa 1 — portar e distribuir

### Diretórios por sistema (`internal/config/paths.go`)
| | Linux (como hoje) | Windows | macOS |
|---|---|---|---|
| config | `$XDG_CONFIG_HOME/sessionflow` ou `~/.config/sessionflow` | `%APPDATA%\SessionFlow` | `~/Library/Application Support/SessionFlow` |
| dados (index.db, user.db) | `~/.local/share/sessionflow` | `%LOCALAPPDATA%\SessionFlow\data` | `~/Library/Application Support/SessionFlow` |
| estado/log | `~/.local/state/sessionflow` | `%LOCALAPPDATA%\SessionFlow\logs` | `~/Library/Logs/SessionFlow` |
| cache (downloads da atualização) | `~/.cache/sessionflow` | `%LOCALAPPDATA%\SessionFlow\cache` | `~/Library/Caches/SessionFlow` |

`ResolvePaths` ganha o GOOS como parâmetro (testável em qualquer SO). XDG continua só no Linux.

### Raízes dos providers (`DefaultProviderRoot`)
- Claude Code: `$CLAUDE_CONFIG_DIR` ou `<home>/.claude` em todos.
- Codex: `$CODEX_HOME` ou `<home>/.codex` em todos.
- OpenCode: `$XDG_DATA_HOME/opencode` ou `<home>/.local/share/opencode` também no macOS e no Windows (o OpenCode usa `xdg-basedir`, que não muda por SO). **A confirmar na tarefa de discovery** (ler o código/docs atuais do OpenCode); se divergir, usar o valor confirmado e documentar em `docs/PROVIDERS.md`.
- Executáveis das CLIs: procurar `claude(.exe)`, `codex(.exe)`, `opencode(.exe)` no PATH e nos caminhos de instalação conhecidos de cada SO (a levantar no discovery). No Windows, **só `.exe`** é aceito para retomar: shims `.cmd`/`.ps1` do npm exigiriam shell (regra 8). Se só houver `.cmd`, a retomada mostra o comando para copiar e explica o motivo.

### Retomada (`internal/terminal`)
- Windows: Windows Terminal (`wt.exe -d <dir> -- <exe> <args…>`) se existir; senão `conhost.exe`/nova console via `CreateProcess` com `CREATE_NEW_CONSOLE` e `Dir`. Sem `cmd /c`.
- macOS: abrir no Terminal.app sem shell interpolado não é possível por `open -a Terminal` com argv; usar um script `.command` temporário **gerado só com argv validado** é shell — proibido. Solução: suportar iTerm2/Terminal via `osascript` também é shell/AppleScript com string — proibido. **Decisão**: no macOS, emuladores que aceitam argv direto (Ghostty, kitty, WezTerm, Alacritty, `open -na <App> --args`) são detectados; sem nenhum, a retomada mostra o comando para copiar. (Ver "Pergunta em aberto" abaixo.)
- Configuração de terminal personalizado ({dir}/{cmd}) continua valendo nos três.

### Outros pontos de plataforma
- `lockfile` (instância única): Windows via `LockFileEx` (golang.org/x/sys/windows, já transitiva do Wails? confirmar; senão arquivo exclusivo com `O_EXCL` + PID); macOS usa o caminho unix atual.
- `detach`: Windows `CREATE_NEW_PROCESS_GROUP|DETACHED_PROCESS`.
- Watcher (fsnotify): funciona nos três; testar limites de recursão no macOS (FSEvents não; fsnotify usa kqueue → um descritor por arquivo: medir com o volume real de `~/.claude/projects`; se estourar, reduzir para observar diretórios e confiar na reconciliação periódica).
- Caminhos de projeto do Claude (`-home-user-x` codificado) no Windows têm `C--Users-...`: conferir decodificação com fixture.
- Fim de linha CRLF em JSONL no Windows: o leitor já tolera? Testar com fixture CRLF.

### CI (`.github/workflows/`)
- `ci.yml`: matriz `ubuntu-24.04`, `windows-latest`, `macos-latest` com `go test ./...` (race só no Linux) e Vitest uma vez.
- `release.yml` na tag: jobs `linux` (como hoje, em contêiner), `windows` (Wails v3 build + NSIS + zip), `macos` (universal via `lipo`, `codesign --sign -` *ad-hoc*, `.dmg` com `hdiutil`, `.zip` com `ditto -c -k --keepParent`), depois `publish` junta, gera `SHA256SUMS`, assina (`SHA256SUMS.sig`) e cria o release.
- Teste mínimo em cada runner: abrir o binário com `--version` e um `--selftest` (abre bancos num HOME temporário, indexa os fixtures, fecha) — sem janela.

### Documentação
`README.md` (instalação por SO, aviso do SmartScreen e do Gatekeeper com o passo "Abrir mesmo assim"/`xattr -dr com.apple.quarantine`), `docs/INSTALL.md`, `docs/PROVIDERS.md` (caminhos por SO), `docs/ARCHITECTURE.md`, ADR `0002-multiplataforma.md`, `CHANGELOG.md`.

## Etapa 2 — atualização autorizada

### Consentimento e configuração
- `config.json`: `updates: { check: "ask" | "on" | "off", lastCheck, skipVersion }`. Padrão `"ask"`.
- Primeiro uso: diálogo "Verificar automaticamente se há versão nova? Consulta só a página de versões do GitHub; nada sobre você ou suas sessões é enviado." [Sim] [Não]. Nada de rede antes da resposta.
- Com `"on"`: verifica no startup (após 30 s) e a cada 24 h. Configurações ganham a opção e um botão "Verificar agora" (vale mesmo com `"off"`, por ser ação explícita).
- `SESSIONFLOW_NO_UPDATE_CHECK=1` desliga tudo (empacotadores, testes). `SESSIONFLOW_UPDATE_URL` aponta para outro servidor no formato da API de releases (teste ponta a ponta).

### Verificação
- GET `https://api.github.com/repos/chmenegatti/sessionflow/releases/latest` (User-Agent `SessionFlow`, timeout 10 s). Ignora draft/prerelease. Compara semver como o Zeno (`newerVersion`). Erro/limite da API: silêncio (log sem detalhes).
- Resultado: `{version, notes (markdown do release), url, asset (nome, tamanho, URL), kind}`; `kind` = formato instalado detectado.

### Detecção do formato instalado (`kind`)
| kind | Como detecta | Asset |
|---|---|---|
| `appimage` | `$APPIMAGE` definido | `SessionFlow-X.Y.Z-x86_64.AppImage` |
| `deb` | executável em `/usr/bin` ou `/opt` e `dpkg-query -S <exe>` responde | `sessionflow_X.Y.Z_amd64.deb` |
| `rpm` | idem com `rpm -qf <exe>` | `sessionflow-X.Y.Z-1.x86_64.rpm` |
| `tarball` | Linux, nenhum dos anteriores | `sessionflow-X.Y.Z-linux-amd64.tar.gz` |
| `windows-setup` | Windows, executável sob `%LOCALAPPDATA%\Programs\SessionFlow` ou chave `HKCU\Software\Microsoft\Windows\CurrentVersion\Uninstall\SessionFlow` | `…-windows-amd64-setup.exe` |
| `windows-portable` | Windows, outro lugar | `…-windows-amd64-portable.zip` |
| `macos` | macOS, executável dentro de `*.app/Contents/MacOS` | `…-macos-universal.zip` |
| `unknown` | nada acima | só aviso + abrir página |

`dpkg-query`/`rpm` rodam com argv fixo, sem shell.

### Integridade e autenticidade
- CI: chave ed25519 privada no segredo `SESSIONFLOW_SIGNING_KEY`; `publish` assina o conteúdo de `SHA256SUMS` → `SHA256SUMS.sig` (assinatura raw em base64). Ferramenta: um pequeno `cmd/sign` em Go (stdlib) no próprio repo.
- App: chave pública embutida (`internal/update/pubkey.go`). Baixa `SHA256SUMS` e `.sig`, verifica com `ed25519.Verify`; baixa o asset para o cache, confere tamanho e SHA-256 contra a linha do `SHA256SUMS`. Qualquer falha: apaga o arquivo e mostra "Atualização recusada: arquivo não confere" — nunca instala.
- Downloads só de `https://github.com/chmenegatti/sessionflow/releases/download/…` (ou `SESSIONFLOW_UPDATE_URL` em teste); redirects para `objects.githubusercontent.com` aceitos só via HTTPS.
- Geração da chave: feita pelo usuário (fora do app), instruções em `docs/RELEASING.md`. Rotação de chave = nova versão com nova pública (documentado).

### Fluxo na interface
1. Banner discreto no topo/rodapé: "SessionFlow X.Y.Z disponível" [Ver novidades] [Atualizar] [Pular esta versão] [Depois].
2. "Ver novidades": painel com as notas (Markdown, mesmo renderer do viewer, sem HTML cru).
3. "Atualizar": confirmação com o que vai acontecer no formato detectado (ex.: "O app vai fechar, instalar e abrir de novo" / "Vai pedir sua senha de administrador"). Barra de progresso do download, verificação, instalação.
4. Erros mostram mensagem e o link da página do release.

### Instalação por formato (`internal/update/install_*.go`)
- `appimage`: grava o novo AppImage ao lado (`.new`), `chmod 0755`, `rename` atômico sobre `$APPIMAGE`, relança o novo e encerra.
- `tarball` / `windows-portable`: extrai para pasta temporária ao lado da instalação; troca executável e arquivos (no Windows o `.exe` em uso é renomeado para `.old` antes, e apagado no próximo start); relança.
- `windows-setup`: executa o `setup.exe` baixado com `/S` (NSIS silencioso) via `CreateProcess` desacoplado; o app encerra; o NSIS reabre o app ao fim (`/S` + flag de relançar no script NSIS). Instalação por usuário: sem UAC.
- `macos`: extrai o `.zip` com `ditto -x -k` para pasta temporária, confere `codesign --verify` *ad-hoc*, substitui o `.app` atual (rename do antigo para lixo temporário, move o novo), remove `com.apple.quarantine` do novo (arquivo baixado pelo app não recebe quarentena, mas garantir), relança via `open -n <app>` e encerra. Sem permissão de escrita (app em `/Applications` de outro usuário): cai para "Abrir o .dmg" e instruções.
- `deb` / `rpm`: `pkexec apt-get install -y <arquivo>` / `pkexec dnf install -y <arquivo>` (ou `rpm -U`), argv separado; o polkit pede a senha. Sem `pkexec`: abre o arquivo no instalador do sistema (`xdg-open`) com instruções. Depois de sucesso: oferece "Reiniciar agora".
- Em todos: dados (`index.db`, `user.db`, config) não são tocados; migrations rodam no próximo start como já acontece.

### Testes
- Unitários: `newerVersion`, detecção de `kind` (com env/paths injetados), parse do `SHA256SUMS`, verificação ed25519 (válida, adulterada, chave errada), escolha do asset por kind/versão, consentimento (nenhuma requisição com `ask`/`off`; `httptest` contando requisições).
- Integração: servidor `httptest` imitando a API + downloads; fluxo completo até "pronto para instalar" em cada SO no CI; instalação real no CI para `tarball` (Linux), `windows-portable` (Windows) e `macos` (macOS) num diretório temporário, relançando com `--version`.
- `deb`/`rpm`/`appimage`/`windows-setup`: teste de instalação nos contêineres/runners existentes com um release falso local (dois builds de versões diferentes), ou documentado como manual se o CI não permitir `pkexec`.

## O que fica a critério de quem implementa
- Nomes exatos de serviços Wails e eventos (`update:available`, `update:progress`).
- Layout fino do banner e do painel.
- Biblioteca de NSIS do template Wails v3 vs script próprio (desde que gere setup silencioso com relançamento).
- Ordem das tarefas dentro de cada etapa.

## Pergunta em aberto
- **Retomada no macOS** sem emulador que aceite argv (só Terminal.app): proposta = mostrar o comando para copiar. Alternativa seria AppleScript com o comando montado em string (quebra a regra "sem shell/interpolação"). Assumido: copiar.
