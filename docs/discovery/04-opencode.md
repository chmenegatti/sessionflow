# 04 — OpenCode

> Fase 0 · 2026-11-21 · Versão local: **opencode 1.14.24** (4 sessões, 167 mensagens, 643 parts)
> Legenda: **[DOC]** opencode.ai/docs · **[CLI]** `--help` local · **[SRC]** github.com/anomalyco/opencode tag v1.14.24 (e `dev`) · **[OBS]** cópia do banco local · **[NÃO VERIFICADO]**

## 1. Aviso principal — armazenamento instável

| Período | Armazenamento | Origem |
|---|---|---|
| < v1.2.0 | arquivos JSON em `<data>/storage/{session,message,part}/…` | [SRC] `storage/json-migration.ts` |
| **v1.2.0 (2026-02-14) → v1.x** | **SQLite** `opencode.db` (drizzle), tabelas `session`/`message`/`part` | [DOC] release notes · PR #10597 |
| **v2.x (dev, v2.3 em 2026-11-18)** | novo esquema `session_message`, `session_part`, `blob`, … em `packages/core` | [SRC] dev |

Repo canônico: **anomalyco/opencode** (`sst/opencode` redireciona). Versão local 1.14.24 está atrás da linha v1 (1.19.6) e da v2.
**Consequência:** adaptador deve detectar esquema e recusar com status claro ("versão de armazenamento não suportada") em vez de quebrar. Interface mais estável: `opencode export <id>` (JSON).

## 2. Localização

| Item | Valor | Origem |
|---|---|---|
| Diretório de dados | `$XDG_DATA_HOME/opencode` → padrão `~/.local/share/opencode` | [SRC] `global/index.ts` |
| Banco | `opencode.db` (canais latest/beta/prod) ou `opencode-<canal>.db` | [SRC] `storage/db.ts` |
| Override | `OPENCODE_DB` (absoluto, relativo ao data dir, ou `:memory:`) | [DOC][SRC] |
| Caminho efetivo | `opencode db path` | [CLI] |
| PRAGMAs | `journal_mode=WAL`, `synchronous=NORMAL`, `busy_timeout=5000`, `foreign_keys=ON` | [SRC] |
| Resto | `storage/session_diff/ses_*.json`, `snapshot/` (repos git), `log/` | [OBS] |
| Credenciais | `auth.json`; tabelas `account`/`control_account` com tokens — **nunca ler** | [OBS] |

## 3. Esquema v1 (1.14.24) [SRC][OBS]

- `project(id, worktree, vcs, name, time_*)` — `id` = hash de 40 chars (repo git) ou valor curto para não-git [OBS].
- `session(id, project_id, parent_id, slug, directory, title, version, share_url, summary_*, revert, permission, time_created, time_updated, time_compacting, time_archived, workspace_id)` — timestamps em **ms**.
- `message(id, session_id, time_created, time_updated, data JSON)` — `data.role` ∈ `user|assistant`.
- `part(id, message_id, session_id, time_*, data JSON)` — `data.type` ∈ `text, subtask, reasoning, file, tool, step-start, step-finish, snapshot, patch, agent, retry, compaction` [SRC `session/message-v2.ts`]; observados: `tool` 165, `step-start` 145, `step-finish` 145, `reasoning` 136, `text` 52 [OBS].
- `tool.state.status` ∈ `pending|running|completed|error`. [SRC]
- `parent_id` em `session` = subsessão (ex.: tool `task` gera sessão filha; `state.metadata.sessionId` aponta para ela) [OBS].
- Identificação de versão: tabela `__drizzle_migrations` (13 linhas localmente, última `20260423070820_add_icon_url_override`).

### IDs [SRC `id/id.ts`]
`ses_` + 12 hex (tempo; sessões usam forma **decrescente**) + 14 base62. Prefixos: `msg_`, `prt_`, `wrk_`, `ent_`. Ordenar por `time_*`, nunca pelo ID.

### Exemplos anonimizados
Ver [`fixtures/opencode/schema-v1.sql`](fixtures/opencode/schema-v1.sql).

```json
// message.data (assistant)
{"role":"assistant","time":{"created":1700000000000,"completed":1700000000000},"parentID":"msg_<id>","modelID":"claude-opus-4.6","providerID":"github-copilot","mode":"build","agent":"build","path":{"cwd":"<str:30>","root":"<str:30>"},"cost":0,"tokens":{"total":11726,"input":11440,"output":286,"reasoning":0,"cache":{"read":0,"write":0}},"finish":"<str:10>"}
// part.data (tool)
{"type":"tool","callID":"<str:30>","tool":"task","state":{"status":"completed","input":{"description":"<str:25>","prompt":"<str:684>","subagent_type":"<str:7>"},"output":"<str:9164>","title":"<str:25>","metadata":{"sessionId":"ses_<id>","truncated":false},"time":{"start":1700000000000,"end":1700000000000}}}
// part.data (text / reasoning / step-finish)
{"type":"text","text":"<str:20>"}
{"type":"reasoning","text":"<str:359>","metadata":{"anthropic":{"signature":"<str:64>"}},"time":{"start":1700000000000,"end":1700000000000}}
{"type":"step-finish","reason":"tool-calls","snapshot":"<str:40>","cost":0,"tokens":{"total":5465,"input":5059,"output":406}}
```

## 4. Leitura segura do banco [EXP — ver doc 05 §4]
- `?mode=ro` lê com OpenCode rodando (WAL), mas **cria `-shm`** se ausente e **falha** em diretório somente leitura sem `-shm`.
- Estratégia: `mode=ro` se `-shm` existir; senão snapshot (copiar `.db`+`-wal`) para cache e ler cópia.
- Selecionar **somente** `project`, `session`, `message`, `part`. Nunca `account*`, `session_share.secret`.

## 5. Retomada

| Comando | Comportamento | Origem |
|---|---|---|
| `opencode -s <id>` / `--session <id>` | abre TUI na sessão | [CLI] |
| `opencode -c` | última sessão | [CLI] |
| `--fork` | fork ao continuar | [CLI] |
| `opencode run -s <id> [--dir]` | não interativo | [CLI] |
| `opencode export [id] [--sanitize]` | JSON `{info, messages: WithParts[]}` | [CLI][SRC] |
| `opencode session list --format json` | listagem | [CLI] |

- `Session.get` busca por ID globalmente; listagem é filtrada por projeto. [SRC]
- **[NÃO VERIFICADO]** se `opencode -s <id>` a partir de outro diretório muda para `session.directory` → SessionFlow executa com `Dir = session.directory`.
- **Não executar `opencode export` na Fase 0**: a CLI abre o banco e pode rodar migrações (escrita). Avaliar na Fase 2 com cópia isolada (`OPENCODE_DB` apontando para snapshot).

## 6. Normalização (resumo)
- `id = "opencode:" + session.id`; `projectPath = session.directory`; `title = session.title`; `createdAt/updatedAt = time_created/time_updated` (ms); `model` = último `message.data.modelID`; arquivada = `time_archived != null`.
- Mensagem = `message` + parts ordenados por `(time_created, id)`.
- Parts: `text`→Text; `reasoning`→Reasoning (colapsado); `tool`→ToolCall + ToolResult (`state.input` / `state.output`); `file`→ImageReference/Metadata; `step-*`, `snapshot`, `patch`, `retry`, `compaction`, `agent`, `subtask` → Metadata.
- Subsessões (`parent_id`) tratadas como subagentes (igual Claude).
- Legado JSON (< 1.2.0): **fora do MVP** — OpenCode migra sozinho e mantém os originais.

## Fontes
- https://opencode.ai/docs/troubleshooting/ · https://opencode.ai/docs/cli/
- https://github.com/anomalyco/opencode/tree/v1.14.24/packages/opencode/src — `storage/db.ts`, `storage/json-migration.ts`, `session/session.sql.ts`, `session/message-v2.ts`, `id/id.ts`, `global/index.ts`, `cli/cmd/export.ts`
- https://github.com/anomalyco/opencode/pull/10597 · https://github.com/anomalyco/opencode/releases/tag/v1.2.0
- https://github.com/anomalyco/opencode/blob/dev/packages/core/src/session/sql.ts
