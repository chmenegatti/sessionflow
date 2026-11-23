# Fixtures de descoberta

Fixtures **sintéticos**, montados a partir dos formatos observados na Fase 0. Nenhum conteúdo de conversa real. Caminhos, IDs e textos são fictícios.

| Arquivo | Provider / versão observada | Casos cobertos |
|---|---|---|
| `claude/session.jsonl` | Claude Code 2.1.330 | prompt com `isMeta`; resposta dividida em várias linhas com o mesmo `message.id` (`thinking` + `tool_use`); `tool_result` ligado por `tool_use_id`; texto com bloco de código; `system/turn_duration`; `ai-title`; **tipo desconhecido**; **última linha truncada, sem `\n`** (escrita em andamento) |
| `codex/rollout.jsonl` | Codex CLI 0.156.1 (formato visto na 0.114.0) | `session_meta`; mensagens `developer` e `user` com contexto injetado; `event_msg/user_message` limpo; `reasoning` criptografado; `function_call` com sua saída; mensagem do assistente duplicada em `agent_message`; **item desconhecido** |
| `codex/history.jsonl` | Codex CLI | histórico auxiliar, que **não é transcript** |
| `opencode/schema-v1.sql` | OpenCode 1.14.24 (esquema drizzle v1) | `project`/`session`/`message`/`part`; parts `text`, `step-start`, `reasoning`, `tool`, `step-finish` e **tipo desconhecido** |

Os testes de cada adaptador (`internal/provider/*`) e o teste de integração do `internal/indexer` carregam estes arquivos. O fixture do OpenCode é executado num banco temporário. Casos extras (JSON inválido, subagente, arquivado, esquema v2, tool com erro) são montados dentro dos testes.
