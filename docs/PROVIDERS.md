# Providers

Fonte detalhada: [discovery/](discovery/00-relatorio.md) (02 Claude Code, 03 Codex, 04 OpenCode). Adaptadores em `internal/provider/{claude,codex,opencode}` (Fase 2).

| | Claude Code | OpenAI Codex | OpenCode |
|---|---|---|---|
| Raiz padrão | `~/.claude` | `~/.codex` | `~/.local/share/opencode` |
| Override de ambiente | `CLAUDE_CONFIG_DIR` | `CODEX_HOME` | `XDG_DATA_HOME`, `OPENCODE_DB` |
| Override no app | `config.json` → `providers.claude.rootDir` | `providers.codex.rootDir` | `providers.opencode.rootDir` |
| Armazenamento | `projects/<dir>/<uuid>.jsonl` | `sessions/YYYY/MM/DD/rollout-*.jsonl` | SQLite `opencode.db` (WAL) |
| Retomar (Fase 6) | `claude --resume <uuid>` no diretório do projeto | `codex resume <uuid>` no `cwd` da sessão | `opencode <projeto> --session <ses_…>` no diretório do projeto |
| Estabilidade do formato | interno, muda por versão | interno | esquema v2 a caminho |

## Implementação (Fase 2)

| | Claude Code | OpenAI Codex | OpenCode |
|---|---|---|---|
| Fontes | `projects/*/*.jsonl` (ignora `*.orphaned-*`) + `projects/*/<id>/subagents/agent-*.jsonl` | `sessions/**/rollout-*.jsonl` + `archived_sessions/**` | `opencode*.db` ou `$OPENCODE_DB` |
| ID nativo | nome do arquivo; subagente = `<pai>/agent-<id>` | UUID do nome do arquivo, ou `session_meta.id` | `session.id` |
| Projeto | 1º `cwd` das linhas | `session_meta.cwd` (ou `turn_context.cwd`) | `session.directory` |
| Título | `custom-title` > `ai-title` > 1º prompt | 1º `event_msg/user_message` | `session.title` > 1º prompt |
| Modelo | último `message.model` (exceto `<synthetic>`) | último `turn_context.model` | último `modelID` do assistente |
| Mensagem do usuário | `user` sem `isMeta`, com texto | `event_msg/user_message` | `role=user` com part `text` |
| Agrupamento | linhas `assistant` consecutivas com o mesmo `message.id` | um item por mensagem | parts da mensagem |
| Pai | diretório `subagents/` | — (não verificado no formato) | `session.parent_id` |
| Arquivada | — | caminho em `archived_sessions/` | `time_archived` |
| Não suportado | — | `*.jsonl.zst` (só contado no `Detail`) | esquema v2 e JSON legado (< 1.2) |
| CLI procurada em | config → `PATH` → `~/.local/bin/claude` | config → `PATH` → `~/.local/bin/codex` | config → `PATH` → `~/.opencode/bin/opencode`, `~/.local/bin/opencode` |

Regras comuns:
- `MessageCount` = mensagens do usuário + respostas do assistente com texto. Passos só de ferramenta, resultados, metadados e contexto injetado não contam.
- `Automated` = sem prompt humano (Claude: também `entrypoint` `sdk*`). Sessões automáticas do Claude recebem como título a 1ª linha do prompt injetado (`isMeta`).
- `sync_state.status`: `ok`, `partial` (última linha incompleta), `error` (relido a cada execução), `duplicate` (mesma sessão já veio de outra fonte; relido a cada execução), `unsupported`.
- Nada é descartado: registros conhecidos sem representação própria (Claude: `attachment`, `last-prompt`, `queue-operation`…; Codex: `event_msg/*`, `token_count`, contexto injetado; OpenCode: `step-*`, `snapshot`…) viram mensagens `system` com bloco `metadata` e, no viewer, o JSON original.
- Imagens: `image_ref` com o tipo MIME (Claude `source.media_type`, também dentro de `tool_result`; OpenCode part `file` com `mime image/*`). Anexos: `file` (Claude `document`, OpenCode part `file`). O conteúdo (base64/URL) nunca é copiado. Campos do part `file` do OpenCode vêm do código-fonte, sem dado local para conferir.
- Tipo desconhecido vira `BlockUnknown` (com o JSON original até 64 KB); JSON inválido é contado e pulado; a última linha sem `\n` é escrita em andamento (`partial`).
- Indexado para busca: texto, nome + entrada da ferramenta (1 KB), resultado da ferramenta (4 KB), até 16 KB por mensagem. Não indexado: reasoning, metadados, desconhecidos, imagens.

## Consumo de tokens (Fase 8)

| | Claude Code | OpenAI Codex | OpenCode |
|---|---|---|---|
| Fonte | `message.usage` das linhas `assistant` | `event_msg/token_count`: `last_token_usage` (pedido) e `total_token_usage` (acumulado) | `message.data.tokens` das respostas |
| Deduplicação | por `message.id` (resposta em várias linhas, mesmo usage) | acumulado que não avança = repetido | por mensagem; `step-finish` ignorado |
| Não informa | reasoning separado | cache criado | — (reasoning: inclusão na saída não verificável localmente) |
| Derivado | total = entrada + saída + cache lido + cache criado | entrada = `input_tokens − cached_input_tokens` | total, quando ausente e reasoning = 0 |

Detalhes, regras de qualidade e limitações em [ANALYTICS.md](ANALYTICS.md).

## Windows e macOS (0.2.0)

Levantado em 2026-11-23 pela documentação oficial; o que não tem fonte oficial está marcado e é conferido no roteiro manual (`docs/manual-tests/0.2.0.md`).

| | Claude Code | OpenAI Codex | OpenCode |
|---|---|---|---|
| Histórico (Windows) | `%USERPROFILE%\.claude` (ou `CLAUDE_CONFIG_DIR`) | `%USERPROFILE%\.codex` (ou `CODEX_HOME`) | `%USERPROFILE%\.local\share\opencode` (ou `XDG_DATA_HOME\opencode`) — documentação oficial de troubleshooting |
| Histórico (macOS) | `~/.claude` | `~/.codex` | `~/.local/share/opencode` (não usa `~/Library`) |
| CLI (Windows) | `PATH` → `%USERPROFILE%\.local\bin\claude.exe` (instalador nativo, documentação oficial) | `PATH` (instalador `install.ps1` ou npm; local exato sem fonte oficial — conferir no roteiro) | `PATH` → `%USERPROFILE%\.opencode\bin\opencode.exe` |
| CLI (macOS) | `PATH` → `~/.local/bin/claude` (link para `~/.local/share/claude/versions/`), `~/.claude/local/claude` | `PATH` → `~/.local/bin/codex`, `/opt/homebrew/bin/codex`, `/usr/local/bin/codex` (Homebrew cask) | `PATH` → `~/.opencode/bin/opencode`, `~/.local/bin/opencode`, `/opt/homebrew/bin/opencode` |

- **Só `.exe` retoma no Windows.** Instalações por npm criam *shims* `.cmd`/`.ps1` (ex.: `%APPDATA%\npm\codex.cmd`), que só rodam por um shell; a retomada mostra o comando para copiar (regra de não usar shell).
- **Projeto do Claude no Windows:** o diretório em `projects/` troca todo caractere que não é letra ou número por `-` (`C:\Users\José Silva\proj` → `C--Users-Jos--Silva-proj`). O SessionFlow nunca decodifica esse nome: o caminho vem do `cwd` das linhas (fixture `docs/discovery/fixtures/windows`).
- **CRLF:** o leitor JSONL aceita linhas terminadas em `\r\n` (teste com o fixture do Windows).

## Retomada (Fase 6)

O `ResumeService` valida a sessão e monta `{executável, argumentos, diretório}`; o `terminal.ExecLauncher` abre um emulador com exec direto, sem shell. Validado em 2026-11-22 com as CLIs instaladas, cada uma num pseudo-terminal com o argv exato do serviço, sem digitar nada e encerrando em seguida:

| | Claude Code 2.1.330 | OpenAI Codex 0.156.1 | OpenCode 1.14.24 |
|---|---|---|---|
| Argumentos | `--resume <uuid>` | `resume <uuid>` | `<projeto> --session <id>` |
| Formato de ID aceito | UUID | UUID (tem precedência sobre nome de sessão na CLI) | `ses_` + 8–64 alfanuméricos |
| Diretório | projeto da sessão (o Claude acha a sessão de qualquer lugar, mas as ferramentas do agente atuam no diretório atual) | `cwd` da sessão (fora dele a TUI pergunta qual usar) | projeto da sessão (também passado como posicional; é absoluto, então não vira opção) |
| Resultado | sessão carregada (título na tela), sem erro | sessão carregada, sem erro | sessão carregada, sem erro |
| Efeito no histórico só de abrir | conteúdo igual (mesmo hash), **mtime atualizado** | **acrescenta um `event_msg/thread_settings_applied`** (~2 KB) ao rollout | atualiza `project.time_updated` no banco; sessões e mensagens iguais |
| CLI procurada em | config → `PATH` → `~/.local/bin/claude`, `~/.claude/local/claude` | config → `PATH` → `~/.local/bin/codex` | config → `PATH` → `~/.opencode/bin/opencode`, `~/.local/bin/opencode` |

Limitações por provider:
- **Claude Code**: dois processos na mesma sessão podem perder mensagens (doc oficial). O SessionFlow pede confirmação se o arquivo mudou há menos de 1 minuto. Abrir a sessão atualiza o mtime e dispara uma releitura do arquivo (adiada se for grande, ver ARCHITECTURE).
- **Codex**: a retomada grava no rollout mesmo sem mensagem nova (evento de configuração da thread); o índice registra como metadado. Instalações via gerenciador de versões (asdf, mise) expõem um *shim* (script); ele é executado como qualquer executável, sem shell do SessionFlow.
- **OpenCode**: não há arquivo por sessão; "sessão ativa" usa só a data da última mensagem. A CLI grava no banco ao abrir (data do projeto). Subsessões não são retomadas sozinhas.
- **Todos**: subagentes/subsessões não são retomados (retome a principal). Diretório de trabalho inexistente bloqueia a retomada (rodar o agente em outro diretório faria as ferramentas atuarem nos arquivos errados). Sessões automáticas (SDK) do Claude retomam por ID, como as demais.

## Monitoramento (Fase 6)

| | Claude Code | OpenAI Codex | OpenCode |
|---|---|---|---|
| Diretórios | `projects/` recursivo (pastas novas entram ao serem criadas) | `sessions/` e `archived_sessions/`, recursivos | diretório de dados e o de cada banco, **sem** subpastas |
| Arquivos que disparam releitura | `*.jsonl` | `*.jsonl`, `*.jsonl.zst` | `*.db`, `*.db-wal` (o `-shm` é ignorado: a própria leitura dele o toca) |

## Status no app

`ok` · `not_found` · `unsupported` · `error` · `disabled` · `pending` (antes da 1ª indexação). O Dashboard mostra o status, o número de sessões, fontes com erro e o detalhe (ex.: rollouts `.zst`).

Regras invariáveis: leitura somente (exceção conhecida: com o `-shm` do OpenCode presente, o SQLite em `mode=ro` registra marcas de leitura nele, como qualquer leitor; `.db` e `-wal` não mudam); nunca ler arquivos ou tabelas de credenciais (`.credentials.json`, `auth.json`, tabelas `account*`); nunca abrir um SQLite de terceiro de modo que crie arquivos no diretório dele (ver discovery 05 §4).
