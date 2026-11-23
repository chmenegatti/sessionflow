# 02 — Claude Code

> Fase 0 · 2026-11-21 · Versão local: **2.1.330**
> Legenda: **[DOC]** code.claude.com/docs · **[CLI]** `claude --help` local · **[OBS]** inspeção dos arquivos locais (426 transcripts + 119 de subagentes) · **[NÃO VERIFICADO]**

## 1. Aviso principal

> "The entry format is internal to Claude Code and changes between versions, so scripts that parse these files directly can break on any release." — [DOC] claude-directory

O formato JSONL **não é interface estável**. Alternativas suportadas: `/export`, `claude -p --resume <id> --output-format json`, `transcript_path` em hooks, Agent SDK `listSessions()`/`getSessionMessages()` (TS/Python — sem SDK Go). [DOC]

**Consequência:** parser Go tolerante (campos opcionais, tipos desconhecidos preservados como `Metadata`), testes com fixtures por versão, campo `version` de cada linha registrado para diagnóstico.

## 2. Localização

| Item | Valor | Origem |
|---|---|---|
| Raiz | `~/.claude/projects/` | [DOC] |
| Relocação | `CLAUDE_CONFIG_DIR` → `$CLAUDE_CONFIG_DIR/projects/` | [DOC] |
| Diretório do projeto | cwd com todo caractere não alfanumérico trocado por `-` (`/home/u/proj` → `-home-u-proj`) | [DOC] |
| Nome > 200 chars | truncado em 200 + hash do caminho | [DOC] |
| Override do nome | `CLAUDE_CODE_PROJECT_DIR_NAME` (só com `CLAUDE_CONFIG_DIR`) | [DOC] |
| Transcript | `<projeto>/<session-uuid>.jsonl` | [DOC][OBS] |
| Subagentes | `<projeto>/<session-uuid>/subagents/agent-<id>.jsonl` + `agent-<id>.json` (meta) | [DOC][OBS] |
| Saídas grandes de tool | `<projeto>/<session-uuid>/tool-results/` | [DOC][OBS] |
| Ignorar | `*.orphaned-<ts>-<sufixo>.jsonl`, `*.jsonl.superseded-<ts>` | [DOC] |
| Histórico de prompts | `~/.claude/history.jsonl` (prompts digitados + projeto) — **não é transcript** | [DOC] |

### Retenção — risco de produto
`cleanupPeriodDays` (padrão **30 dias**, mínimo 1) apaga transcripts antigos. [DOC] O SessionFlow nunca guarda cópia integral → sessão apagada pelo Claude some do índice no próximo scan. Decisão de produto pendente: manter metadados/texto indexado de sessões removidas ("órfãs") ou espelhar a remoção. Recomendação MVP: **espelhar** (o banco é reconstruível; não virar backup).

## 3. Identidade da sessão [OBS]
- Nome do arquivo = `sessionId` em **100%** dos 426 arquivos; nenhum arquivo com mais de um `sessionId`.
- Diretório do projeto ≠ fonte confiável do cwd: em 11/426 o `cwd` da 1ª linha não bate com o nome codificado (sessão iniciou em outro dir ou nome truncado). **Usar `cwd` das linhas, não decodificar o diretório** (decodificação é ambígua: `-` pode ser `/`, `.`, `_`, espaço).
- Subagente: mesmo `sessionId` do pai, `isSidechain: true`, campo `agentId`.
- 246/426 arquivos sem nenhuma mensagem de usuário "real" (`isMeta` ausente/false) — sessões de SDK, observadores, hooks. Filtrar/agrupar como "automáticas" na UI. `entrypoint` (`cli`, `sdk-ts`, …) ajuda a classificar.

## 4. Formato JSONL [OBS]

Uma linha = um objeto com `type`. Tipos encontrados (contagem global):

| `type` | Qtde | Uso no SessionFlow |
|---|---|---|
| `user` | 30 643 | mensagem (texto do usuário **ou** `tool_result`) |
| `assistant` | 54 157 | mensagem (blocos `text`, `thinking`, `tool_use`) |
| `system` | 4 083 | metadado; subtipos `compact_boundary`, `turn_duration`, `stop_hook_summary`, `away_summary`, `informational`, `local_command`, `api_error`, … |
| `ai-title` | 9 791 | **título** (`aiTitle`) — último vence |
| `custom-title` | 108 | título definido pelo usuário — prioridade sobre `ai-title` |
| `last-prompt` | 18 372 | `leafUuid` da folha atual |
| `attachment` | 64 747 | contexto injetado — ignorar no MVP |
| `queue-operation`, `api-request*`, `mode`, `permission-mode`, `session-owner`, `atis-latch`, `cost-state`, `pr-link`, `file-history-*`, `bridge-session`, `agent-name`, `tools-used`, `continued-in`, … | — | ignorar (ou extrair metadado pontual) |

Nenhuma linha JSON inválida em ~400 mil linhas, mas o maior arquivo tem **172 MB** → streaming obrigatório.

### Campos comuns de `user`/`assistant`/`system`
`uuid`, `parentUuid` (árvore; `null` na raiz e após compactação), `logicalParentUuid` (em `compact_boundary`), `isSidechain`, `isMeta`, `timestamp` (ISO-8601), `sessionId`, `cwd`, `gitBranch`, `version`, `entrypoint`, `userType`, `slug`.

### `message`
- `user.message.content`: **string** (prompt) **ou** lista de blocos `text` | `image` | `tool_result{tool_use_id, content, is_error?}`.
- `assistant.message`: `{model, id, role, content[], stop_reason, usage}`; blocos `text{text}`, `thinking{thinking, signature}` (frequentemente `thinking` vazio — só assinatura), `tool_use{id, name, input}`.
- **Uma resposta da API é gravada em várias linhas** `assistant` (um bloco por linha, mesmo `message.id`, `apiBlockIndex` crescente). Agrupar por `message.id` para exibir.
- `user` com `toolUseResult` traz a saída estruturada da ferramenta (formato varia por ferramenta).

### Exemplos anonimizados
Ver [`fixtures/claude/session.jsonl`](fixtures/claude/session.jsonl). Esqueletos reais (strings substituídas por `<str:N>`):

```json
{"type":"assistant","uuid":"<uuid>","parentUuid":"<uuid>","isSidechain":false,"sessionId":"<uuid>","timestamp":"2026-01-01T00:00:00.000Z","cwd":"<str:41>","version":"2.1.329","message":{"model":"claude-sonnet-4-5-20250929","id":"msg_<id>","role":"assistant","content":[{"type":"tool_use","id":"toolu_<id>","name":"ToolSearch","input":{"query":"<str:17>","max_results":2}}],"stop_reason":"tool_use","usage":{"input_tokens":10,"output_tokens":294}},"apiBlockIndex":1,"requestId":"req_<id>"}
{"type":"user","uuid":"<uuid>","parentUuid":"<uuid>","sessionId":"<uuid>","timestamp":"2026-01-01T00:00:00.000Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_<id>","content":"<str:32>"}]},"toolUseResult":{"matches":[],"query":"<str:17>","total_deferred_tools":39},"sourceToolAssistantUUID":"<uuid>"}
{"type":"system","subtype":"compact_boundary","parentUuid":null,"logicalParentUuid":"<uuid>","content":"<str:22>","compactMetadata":{"trigger":"<str:4>","preTokens":176318,"postTokens":81327}}
{"type":"ai-title","aiTitle":"<str:43>","sessionId":"<uuid>"}
```

## 5. Retomada

| Comando | Comportamento | Origem |
|---|---|---|
| `claude --resume <session-id>` | retoma; **funciona de qualquer diretório** (procura projeto atual + worktrees, depois todos os projetos; falha se o ID existir em >1 projeto) | [DOC] (≥ 2.1.223) |
| `claude --resume <caminho-absoluto.jsonl>` | retoma pelo arquivo | [DOC] |
| `claude -c/--continue` | mais recente **do cwd** | [DOC][CLI] |
| `--fork-session` | retoma com novo ID | [DOC][CLI] |

Execução no SessionFlow: `exec.Command(claudePath, "--resume", id)` com `Dir = cwd da sessão` (se existir; senão home), dentro de um terminal (ver relatório §terminal). Sessões de `-p`/SDK não aparecem no picker mas retomam por ID. [DOC]

**Risco:** dois processos retomando a mesma sessão gravam no mesmo arquivo e podem perder mensagens [DOC] → avisar se o arquivo foi modificado há < N segundos (sessão provavelmente ativa).

## 6. Normalização (resumo)
- Sessão: `id = "claude:" + sessionId`; `projectPath = cwd` da 1ª linha com `cwd`; `title = custom-title ?? ai-title ?? 1º prompt real truncado`; `createdAt/updatedAt` = min/max `timestamp`; `model` = último `message.model`; `messageCount` = linhas user/assistant não-meta agrupadas.
- Mensagem: agrupar linhas `assistant` por `message.id`; `tool_result` vira bloco `ToolResult` ligado ao `ToolCall` por `tool_use_id`.
- Subagentes: sessões filhas (`parentSessionId`) — exibidas dentro do viewer como tool call expansível, não na lista principal (MVP).
- Árvore `parentUuid`: MVP exibe ordem do arquivo; ramificações (rewind/edit) ficam fora do MVP.

## Fontes
- https://code.claude.com/docs/en/sessions
- https://code.claude.com/docs/en/claude-directory
- https://code.claude.com/docs/en/agent-sdk/sessions
- https://code.claude.com/docs/en/settings · https://code.claude.com/docs/en/env-vars
