# Fase 0 — Relatório de Technical Discovery

> SessionFlow · 2026-11-21 · Ambiente: Ubuntu 26.04.1 LTS, GNOME/Wayland, Go 1.26.1, Node 25.8, gcc 15.2
> Detalhes por tema: [01 Wails v3](01-wails-v3.md) · [02 Claude Code](02-claude-code.md) · [03 Codex](03-codex.md) · [04 OpenCode](04-opencode.md) · [05 SQLite/FTS5](05-sqlite-fts5.md) · [06 Linux/distribuição](06-linux-distribuicao.md) · [fixtures](fixtures/README.md)
>
> Legenda usada em todos os documentos: **[DOC]** documentação oficial · **[SRC]** código-fonte · **[CLI]** `--help` da versão instalada · **[OBS]** dados locais inspecionados (somente leitura, cópias dos bancos) · **[EXP]** experimento executado · **[NÃO VERIFICADO]**

## 1. Achados que mudam o plano original

1. **Nenhum dos três formatos é interface estável.** Claude declara o JSONL "internal… can break on any release" [DOC]. Codex manda usar o parser canônico deles [SRC]. OpenCode trocou de armazenamento duas vezes: JSON → SQLite (v1.2) → novo esquema SQLite (v2) [DOC/SRC]. → Parsers tolerantes, detecção de versão, status "não suportado" por provider em vez de crash.
2. **OpenCode não usa arquivos de sessão: usa SQLite (`opencode.db`, WAL).** Codex também mantém SQLite (`state_5.sqlite`), mas lá é só espelho do JSONL. → O contrato do provider não pode assumir "um arquivo por sessão".
3. **Abrir SQLite de terceiro com `mode=ro` pode criar `-shm` no diretório do provider** e falha em montagem somente leitura [EXP]. → Leitura via snapshot quando `-shm` não existe.
4. **Wails v3 é estável (v3.1.0) e usa GTK4 + WebKitGTK 6.0 por padrão.** O template se chama `react` (não `react-ts`) e o pacote `libwebkitgtk-6.0-dev` falta nesta máquina [SRC/OBS].
5. **Flatpak entra em conflito com o produto.** Retomar sessão exige rodar a CLI do host (`flatpak-spawn --host`, que é escape do sandbox) e o Flathub tende a rejeitar. O Wails não tem tooling Flatpak. → MVP com AppImage + .deb; Flatpak adiado.
6. **Claude apaga transcripts após 30 dias** por padrão (`cleanupPeriodDays`) [DOC].
7. **Volume real:** transcript Claude de até **172 MB** e 246 de 426 sessões sem prompt humano (SDK/automação) [OBS].

## 2. Matriz comparativa dos providers

| | Claude Code 2.1.330 | Codex CLI 0.156.1 | OpenCode 1.14.24 |
|---|---|---|---|
| Raiz | `~/.claude/projects/` | `~/.codex/sessions/` | `~/.local/share/opencode/` |
| Override | `CLAUDE_CONFIG_DIR` [DOC] | `CODEX_HOME`, `CODEX_SQLITE_HOME` [DOC] | `XDG_DATA_HOME`, `OPENCODE_DB` [DOC/SRC] |
| Armazenamento | 1 JSONL por sessão | 1 JSONL ("rollout") por sessão, em `YYYY/MM/DD/` (+ `.jsonl.zst` futuro) | SQLite WAL `session`/`message`/`part` (`data` JSON) |
| Índice auxiliar | `history.jsonl` (só prompts) | `history.jsonl` (só prompts) + `state_5.sqlite.threads` (espelho) | — (o banco é a fonte) |
| ID da sessão | UUID v4 = nome do arquivo | UUID v7 em `session_meta.id` = sufixo do arquivo | `ses_` + 26 chars |
| Projeto | `cwd` nas linhas (dir codificado não é confiável) | `session_meta.cwd` | `session.directory` / tabela `project` |
| Título | `custom-title` > `ai-title` | `threads.name`/`title` ou 1º prompt | `session.title` |
| Modelo | `message.model` (assistant) | `turn_context.model` | `message.data.modelID` |
| Timestamps | ISO-8601 por linha | ISO-8601 por linha | epoch ms |
| Mensagem do usuário | `user` com `content` string/blocos; `isMeta` = injetado | `event_msg/user_message` (limpo) | `message.role=user` + parts `text` |
| Resposta | várias linhas `assistant` por `message.id` | `response_item/message assistant` (duplicado em `agent_message`) | `message.role=assistant` + parts |
| Tool call / result | `tool_use` ↔ `tool_result` (`tool_use_id`) | `function_call`/`custom_tool_call` ↔ `*_output` (`call_id`) | part `tool` com `state.input`/`state.output` no mesmo registro |
| Reasoning | `thinking` (muitas vezes vazio + assinatura) | `encrypted_content` | part `reasoning` (texto) |
| Subagentes | `<sessão>/subagents/agent-*.jsonl` | `thread_spawn_edges`, `parent_thread_id` [SRC] | `session.parent_id` |
| Compactação | `system/compact_boundary` | `compacted` | part `compaction`, `time_compacting` |
| Arquivada | — | `archived_sessions/`, `threads.archived` | `time_archived` |
| Retomar | `claude --resume <id>` (qualquer dir) | `codex resume <id>` (qualquer dir; pergunta o cwd se diferente) | `opencode -s <id>` (dir: [NÃO VERIFICADO]) |
| Retenção | **30 dias padrão** | sem limpeza automática conhecida | sem limpeza automática conhecida |
| Estabilidade | interna, muda por versão [DOC] | interna, migrando para "paginated" [SRC] | em migração para esquema v2 [SRC] |
| Dados sensíveis vizinhos | `.credentials.json` | `auth.json` | `auth.json`, tabelas `account*` |

## 3. Exemplos anonimizados

- Esqueletos reais (strings trocadas por `<str:N>`, IDs por `<uuid>`) estão em cada doc de provider, na seção "Exemplos anonimizados".
- Fixtures sintéticos versionáveis: `fixtures/claude/session.jsonl`, `fixtures/codex/rollout.jsonl`, `fixtures/codex/history.jsonl`, `fixtures/opencode/schema-v1.sql`. Eles cobrem tipos desconhecidos, linha truncada e respostas divididas em várias linhas.

## 4. Estratégia de normalização

### Modelo interno
```
Session  { Key{Provider, NativeID}, ParentKey?, ProjectPath, Title, FirstPrompt, CreatedAt, UpdatedAt,
           MessageCount, Model, Source{Kind: jsonl|sqlite, Path, NativeRef}, Archived, IsAutomated }
Message  { Seq, NativeID, Role(user|assistant|system|tool), Timestamp, Blocks[] }
Block    { Kind(text|code|tool_call|tool_result|reasoning|image_ref|metadata|unknown),
           Text, ToolName, ToolCallID, ToolInput(JSON), IsError, Raw(JSON, opcional) }
```

### Regras
1. **Identidade** = `(provider, native_id)`. O ID interno da UI vem como `provider:native_id`. Sem colisões e sem precisar de hash.
2. **Projeto** = caminho absoluto limpo (`filepath.Clean`, sem barra final) do cwd da sessão. Projetos de providers diferentes se juntam pelo mesmo caminho. O nome exibido é `filepath.Base`. Caminho inexistente gera projeto marcado "ausente", sem erro.
3. **Usuário vs. contexto injetado:** Claude usa `isMeta`; Codex usa `event_msg/user_message`; OpenCode usa parts `text` de `role=user`. Sem heurística de texto.
4. **Agrupamento:** linhas `assistant` do Claude se juntam por `message.id`. Tool calls se ligam aos resultados por `tool_use_id`/`call_id`. No OpenCode já vêm juntos.
5. **Ordem** = ordem física (linha do JSONL; `(time_created, id)` no OpenCode). `parentUuid` é guardado, mas a ramificação fica fora do MVP.
6. **Desconhecido** vira `Block{Kind: unknown, Raw}` e aparece recolhido no viewer. O parser nunca falha por causa de um tipo novo.
7. **Texto indexável:** text + nome e entrada resumida das tools + resultado truncado (~4 KB). Exclui reasoning criptografado/assinatura, base64 e `attachment`.
8. **Sessões automáticas** (sem prompt humano, `entrypoint` sdk) ficam marcadas `IsAutomated` e escondidas por padrão.
9. **Subagentes/subsessões** ficam ligados ao pai e não aparecem na lista principal.
10. **Viewer carrega sob demanda:** o índice guarda o texto normalizado. Para exibir com fidelidade, o adaptador relê a fonte (JSONL por offset, SQLite por ID). Assim não se guarda cópia integral (requisito §8).

## 5. Riscos técnicos

| # | Risco | Prob. | Impacto | Mitigação |
|---|---|---|---|---|
| R1 | Formato muda numa atualização do provider | **alta** | parser quebra | parser tolerante; tipo desconhecido vira `unknown`; registrar `version`; fixtures por versão; status "parcial/não suportado" por provider |
| R2 | OpenCode v2 troca o esquema | **alta** (já existe no `dev`) | provider para | detectar esquema por tabelas/migrations; adaptador v1 primeiro; v2 depois ou fallback via `opencode export` |
| R3 | Ler SQLite de terceiro cria arquivo ou trava | média | viola "não modificar" | `mode=ro` só com `-shm` existente; senão snapshot; `busy_timeout`; nunca ler tabelas de credenciais |
| R4 | JSONL gigante (172 MB) e linhas longas | **certa** | memória/latência | streaming com `bufio.Reader` (o `Scanner` padrão tem limite de 64 KB); offsets incrementais; viewer paginado por offset |
| R5 | Linha parcial durante a escrita | **certa** | JSON inválido | consumir só até o último `\n`; reprocessar no próximo evento |
| R6 | Limite de inotify / árvore grande | baixa (65 536 local, 262 dirs) | eventos perdidos | watch só por diretório necessário; fallback para polling; rescan periódico leve (stat) |
| R7 | Claude apaga sessões com 30 dias | **certa** | sessão some | espelhar a remoção; deixar claro na UI que a fonte é o provider |
| R8 | Retomar exige terminal interativo | **certa** | precisa de um emulador | `terminal.Launcher` configurável (padrão `x-terminal-emulator -e`; local: gnome-terminal/ptyxis) |
| R9 | Retomar sessão já aberta | média | perda de mensagens (Claude [DOC]) | avisar se a fonte mudou há menos de 60 s |
| R10 | Flatpak (sandbox, `:ro`, flatpak-spawn) | **alta** | retomar/ler quebra | adiar; AppImage + .deb no MVP; ADR |
| R11 | Conteúdo de conversa renderizado como HTML/iframe | média | XSS na ponte nativa (GHSA-2p5f-2gx8-mh8m) | markdown sanitizado, sem HTML cru, sem iframe |
| R12 | Logs vazam conteúdo | média | privacidade | logar só caminho, provider, contagens e tipo de erro |
| R13 | Codex `.jsonl.zst` e histórico "paginated" | média (features desligadas hoje) | sessões invisíveis | detectar e reportar "não suportado"; zstd quando a feature ativar |
| R14 | Go 1.26.1 abaixo do recomendado | baixa | correção crypto/x509 ausente | subir para ≥ 1.26.10 antes da Fase 1 |

## 6. Arquitetura recomendada

Monólito modular, como em CLAUDE.md, com estes ajustes:

```
sessionflow/
├── main.go                      # wiring: config → repos → providers → services → Wails
├── internal/
│   ├── domain/                  # Session, Message, Block, Project, ProviderID (sem deps)
│   ├── provider/
│   │   ├── provider.go          # interfaces pequenas (abaixo)
│   │   ├── jsonl/               # leitor streaming com offset e linha parcial (Claude + Codex)
│   │   ├── sqliteread/          # abertura ro/snapshot de bancos de terceiros (OpenCode, Codex state)
│   │   ├── claude/  codex/  opencode/
│   ├── index/                   # SQLite do app: esquema, migrações, FTS5, sync_state
│   ├── service/                 # SessionService, SearchService, ResumeService, SyncService
│   ├── watcher/                 # fsnotify + debounce + fallback polling
│   ├── terminal/                # Launcher (exec direto, sem shell)
│   └── config/                  # XDG paths, settings
└── frontend/ (template wails3 "react" + Tailwind + shadcn/ui)
```

### Contrato refinado dos providers
```go
type Provider interface {
    ID() domain.ProviderID
    Detect(ctx context.Context) (Detection, error)               // dirs, versão de formato, CLI encontrada
    Sources(ctx context.Context) ([]Source, error)                // arquivos JSONL ou "db + session ids"
    Scan(ctx context.Context, src Source, from Cursor) (ScanResult, error) // sessões + mensagens novas desde o cursor
    Load(ctx context.Context, key domain.SessionKey, page Page) (*domain.Transcript, error) // viewer
    ResumeCommand(s domain.Session) (terminal.Command, error)     // só monta argv + dir; não executa
    WatchPaths() []string
}
```
- `Cursor` é opaco: offset de bytes + fingerprint no JSONL; marca d'água `time_updated` no SQLite. Isso acomoda as diferenças reais sem uniformidade forçada.
- `ResumeCommand` devolve `{Path, Args[], Dir}`. Quem executa é o `terminal.Launcher`, que é injetável, testável e fica separado do provider.
- `Resume` some do provider. Sem shell: `exec.Command(term, "--", cli, args...)`.

### Bancos do app
- `$XDG_DATA_HOME/sessionflow/index.db` (reconstruível: sessões, mensagens normalizadas, FTS5, `sync_state`).
- `$XDG_DATA_HOME/sessionflow/user.db` (favoritos, tags, settings; **não** reconstruível, sobrevive a um rebuild do índice).
- Driver: `modernc.org/sqlite` (FTS5 embutido, sem CGO extra) [EXP].

### Sincronização
`ServiceStartup` faz o scan inicial em goroutine (um provider por vez, erros isolados por provider) e depois liga o watcher. Eventos do fsnotify passam por debounce de ~300 ms por fonte e viram `Scan(source, cursor)`. No fim, o evento Wails `sessions:changed` é emitido. Um rescan leve por `stat` a cada N minutos cobre eventos perdidos.

## 7. Plano de implementação — Fase 1 (Foundation)

**Pré-requisitos (precisam de você):** `sudo apt install libwebkitgtk-6.0-dev`; Go ≥ 1.26.10; `go install github.com/wailsapp/wails/v3/cmd/wails3@v3.1.0`; `wails3 doctor` limpo.

| # | Entrega | Verificação |
|---|---|---|
| 1 | `wails3 init -n sessionflow -t react` dentro de `sessionflow/`, versões fixadas (Go e `@wailsio/runtime` 3.1.0) | `wails3 build` gera binário; janela abre |
| 2 | Tailwind v4 (`@tailwindcss/vite`) + shadcn/ui + Lucide + tema dark padrão | componente Button renderiza no WebKitGTK |
| 3 | React Router + Zustand; layout sidebar/main/detalhe com rotas vazias (Dashboard, Sessions, Projects, Favorites, Search, Settings) | navegação funciona; estados vazios |
| 4 | `internal/domain`: tipos da §4 + testes de invariantes (SessionKey, Clean de path) | `go test ./...` |
| 5 | `internal/config`: caminhos XDG do app; detecção dos diretórios dos providers respeitando `CLAUDE_CONFIG_DIR`, `CODEX_HOME`, `OPENCODE_DB`/`XDG_DATA_HOME` | testes com `t.Setenv` |
| 6 | `internal/index`: abertura `modernc.org/sqlite`, WAL, `user_version`, esquema inicial (sessions, messages, messages_fts + triggers, sync_state), `quick_check` → rebuild | testes de migração e de recuperação após corrupção (arquivo truncado) |
| 7 | `user.db` (favoritos/tags/settings) separado | teste: rebuild do índice preserva favoritos |
| 8 | Logging `slog` (JSON em arquivo `$XDG_STATE_HOME/sessionflow/`, texto no dev) sem conteúdo de conversa | revisão + teste de que o handler não recebe conteúdo |
| 9 | Serviço Wails `AppService.Status()` + evento tipado `app:ready`; bindings TS gerados | front exibe versão e status dos providers (só Detect) |
| 10 | Docs: README, `docs/ARCHITECTURE.md`, `docs/DOMAIN.md`, `AGENTS.md`, ADRs 0001 (Wails v3 + GTK4), 0002 (modernc sqlite), 0003 (dois bancos: índice + usuário), 0004 (AppImage/.deb no MVP, Flatpak adiado) | revisão |

**Fora da Fase 1:** parsers (Fase 2), watcher, resume, busca na UI.

## 8. Limitações desta descoberta

- Codex: só um rollout local (gravado pela 0.114.0). Variantes novas (`ordinal`, `.zst`, paginated) foram vistas só no código-fonte.
- OpenCode: não rodei `opencode export` nem `session list` porque a CLI abre o banco real e pode migrar (escrita). Fica para a Fase 2, com `OPENCODE_DB` apontando para um snapshot.
- Não verificado: Tailwind no template Wails v3; Flatpak (runtime GNOME com webkitgtk-6.0, inotify e WAL no sandbox); se `opencode -s` troca de diretório sozinho.
- Nenhum arquivo original foi modificado. Os bancos foram lidos a partir de cópias no scratchpad da sessão.
