# 03 — OpenAI Codex CLI

> Fase 0 · 2026-11-21 · Versão local: **codex-cli 0.156.1** (1 rollout local, gravado pela 0.114.0)
> Legenda: **[DOC]** learn.chatgpt.com · **[CLI]** `--help` local · **[SRC]** github.com/openai/codex HEAD d4014cb (2026-11-21, pode ser mais novo que a 0.156.1) · **[OBS]** arquivos locais · **[NÃO VERIFICADO]**

## 1. Localização

| Item | Valor | Origem |
|---|---|---|
| Raiz | `$CODEX_HOME`, padrão `~/.codex` | [DOC][SRC] `utils/home-dir` |
| Transcripts ("rollouts") | `$CODEX_HOME/sessions/YYYY/MM/DD/rollout-YYYY-MM-DDTHH-MM-SS-<uuid>.jsonl` | [SRC] `rollout/src/lib.rs`, `rollout_file_name.rs` · [OBS] |
| Arquivados | `$CODEX_HOME/archived_sessions/` | [SRC] (ausente localmente) |
| Thread revertida | `rollout-<ts>-<thread_id>_<rollout_id>.jsonl` | [SRC] |
| Compactados | `*.jsonl.zst` (arquivos frios ≥ 7 dias) — feature `local_thread_store_compression`, **desligada** na 0.156.1 | [SRC][CLI] `codex features list` |
| Metadados (índice/cache) | `state_5.sqlite` tabela `threads`; local muda com `CODEX_SQLITE_HOME` / `sqlite_home` | [SRC][DOC] |
| Histórico paginado (novo) | `thread_history_1.sqlite` (`thread_turns`, `thread_items`) — feature `background_paginated_rollout_migration` desligada | [SRC] (ausente localmente) |
| Histórico auxiliar | `$CODEX_HOME/history.jsonl` — **só prompts**, não transcript | [DOC][SRC] |
| Credenciais | `auth.json` — **nunca ler** | [OBS] |

Número de versão no nome do banco (`state_5`) muda entre versões → descobrir por glob `state_*.sqlite` e escolher o maior.

## 2. Fonte de verdade
- `state` crate: "extracts rollout metadata from JSONL rollouts and mirrors it into a local SQLite database" — espelho/cache com `BackupAndRebuild`. [SRC]
- Modo `paginated` torna parte dos metadados autoritativos no SQLite. [SRC]
- **Decisão:** JSONL é a fonte primária; `threads` é atalho opcional para título (`title`, `name`, `first_user_message`) e `archived`. Funcionar sem o banco.
- Comentário no código: leitores devem usar "codex_rollout's canonical parser" → formato interno, instável. [SRC]

## 3. Formato JSONL

Linha: `{"timestamp", "type", "payload"}` (+ `ordinal` opcional em versões novas). `type` = variante de `RolloutItem` (serde `tag="type"`, snake_case). [SRC] `codex-rs/history/src/lib.rs`

Variantes no HEAD [SRC]: `session_meta`, `response_item`, `event_msg`, `turn_context`, `compacted`, `dynamic_tools_updated`, `inter_agent_communication(_metadata)`, `token_usage_record`, `world_state`, `retained_context`, `security_risk_score`, `realtime_item`.

Observado localmente [OBS] (507 linhas):

| `type` / `payload.type` | Qtde | Uso |
|---|---|---|
| `session_meta` | 1 | **id**, cwd, cli_version, source, originator, model_provider, base_instructions |
| `turn_context` | 5 | **model**, cwd, approval/sandbox policy por turno |
| `response_item/message` role `developer` | — | instruções do sistema — ocultar |
| `response_item/message` role `user` | — | prompt **+ contexto injetado** (AGENTS.md, environment_context) |
| `response_item/message` role `assistant` | 52 total | texto (`output_text`), `phase` (`commentary`/final) |
| `response_item/reasoning` | 65 | `encrypted_content` + `summary` (geralmente vazio) — não indexar |
| `response_item/function_call` + `function_call_output` | 106+106 | tool call (`name`, `arguments` string JSON, `call_id`) |
| `response_item/custom_tool_call(+_output)` | 12+12 | ex.: `apply_patch` (`input` texto) |
| `response_item/web_search_call` | 2 | `action.query` |
| `event_msg/user_message` | 5 | **prompt limpo do usuário** |
| `event_msg/agent_message` | 44 | duplica texto do assistant |
| `event_msg/task_started|task_complete|token_count` | — | metadados de turno |

### Duplicação
Texto do usuário aparece em `response_item(user)` (com contexto injetado) **e** em `event_msg/user_message` (limpo); texto do assistente em `response_item(assistant)` **e** `event_msg/agent_message`.
**Regra:** usuário ← `event_msg/user_message`; assistente/tools/reasoning ← `response_item`; demais `event_msg` → metadados. Evita heurística sobre texto injetado.

### Exemplos anonimizados
Ver [`fixtures/codex/rollout.jsonl`](fixtures/codex/rollout.jsonl).

```json
{"timestamp":"2026-01-01T00:00:00.000Z","type":"session_meta","payload":{"id":"<uuid>","timestamp":"2026-01-01T00:00:00.000Z","cwd":"<str:24>","originator":"codex_cli_rs","cli_version":"0.114.0","source":"cli","model_provider":"openai","base_instructions":"<omitido>"}}
{"timestamp":"2026-01-01T00:00:00.000Z","type":"event_msg","payload":{"type":"user_message","message":"<str:74>","images":[],"local_images":[],"text_elements":[]}}
{"timestamp":"2026-01-01T00:00:00.000Z","type":"response_item","payload":{"type":"function_call","name":"exec_command","arguments":"<str:74>","call_id":"call_<id>"}}
{"timestamp":"2026-01-01T00:00:00.000Z","type":"response_item","payload":{"type":"function_call_output","call_id":"call_<id>","output":"<str:127>"}}
```

### `history.jsonl` [OBS][SRC]
`{"session_id":"<uuid>","ts":1773537338,"text":"..."}` — `HistoryEntry{session_id, ts: u64, text}`. `ts` em segundos Unix [OBS: valor compatível com 2026-03]. Desligável por `[history] persistence = "none"`. **Não usar** como fonte de transcript.

### Tabela `threads` [OBS][SRC] (colunas relevantes)
`id` (= uuid do rollout), `rollout_path` (absoluto), `created_at`, `updated_at` (s), `*_ms`, `cwd`, `title`, `name`, `first_user_message`, `preview`, `model`, `archived`, `git_branch`, `git_origin_url`, `cli_version`, `source`, `history_mode`.

## 4. Retomada

| Comando | Comportamento | Origem |
|---|---|---|
| `codex resume <SESSION_ID>` | UUID ou nome; **funciona de qualquer dir** | [CLI][SRC] |
| `codex resume --last` | mais recente **do cwd** (`--all` remove filtro) | [CLI][DOC] |
| `codex exec resume <ID> [PROMPT]` | não interativo | [CLI] |
| `-C/--cd <DIR>` | define diretório de trabalho | [CLI] |
| `codex fork` / `archive` / `delete` | — (SessionFlow nunca chama delete/archive) | [CLI] |

Ao retomar de cwd diferente, a TUI **pergunta** se usa o dir atual ou o original (`tui/src/cwd_prompt.rs`). [SRC] → SessionFlow executa com `Dir = session_meta.cwd` para evitar o prompt.

## 5. Normalização (resumo)
- `id = "codex:" + session_meta.id`; `projectPath = session_meta.cwd`; `model` = último `turn_context.model`; `title = threads.name ?? threads.title ?? 1º user_message truncado`.
- Tool call: `function_call`/`custom_tool_call` ↔ `*_output` por `call_id`.
- `.zst`: suportar leitura via `github.com/klauspost/compress/zstd` (stream) — dependência justificada quando a feature ativar; MVP pode registrar "formato não suportado" e seguir.

## 6. Não verificado
- Se a 0.156.1 já grava `ordinal` e variantes novas (único rollout local é da 0.114.0). Gerar sessão de teste na Fase 2.
- Layout `YYYY/MM/DD` no HEAD (confirmado só no arquivo local).

## Fontes
- https://learn.chatgpt.com/docs/developer-commands?surface=cli
- https://learn.chatgpt.com/docs/config-file/config-advanced.md
- https://github.com/openai/codex — `codex-rs/rollout/src/{lib.rs,rollout_file_name.rs,compression.rs}`, `codex-rs/history/src/{lib.rs,rollout_payload.rs}`, `codex-rs/protocol/src/protocol.rs`, `codex-rs/state/`, `codex-rs/message-history/src/lib.rs`, `codex-rs/tui/src/cwd_prompt.rs`
