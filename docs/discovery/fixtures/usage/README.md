# Fixtures de consumo (Fase 8)

Estrutura dos formatos reais (Claude Code 2.1.330, Codex CLI 0.156.1, OpenCode 1.14.24) com **números de consumo copiados de históricos locais** e todo o resto fictício: textos, caminhos (`/home/user/projects/demo-app`), IDs e UUIDs. Nenhum conteúdo de conversa real.

Valores esperados (usados nos testes e na tabela de verificação). `—` = indisponível (nunca 0).

## Claude Code (`claude/-home-user-projects-demo-app/`)

| Sessão / arquivo | Origem | Resposta | entrada | saída | cache lido | cache criado | reasoning | total | status |
|---|---|---|---|---|---|---|---|---|---|
| `3333….jsonl` (principal) | main_agent | `msg_FIX_A` (3 linhas, uma não contígua) | 2 | 403 | 48.829 | 3.492 | — | 52.726 (derivado) | complete |
| | | `msg_FIX_B` (1 linha) | 2 | 82 | 70.075 | 6.156 | — | 76.315 (derivado) | complete |
| | | `msg_FIX_C` (sem cache_creation) | 3 | 50 | 1.000 | — | — | — | partial |
| | | `msg_FIX_SYN` (`<synthetic>`, zerada) | descartada | | | | | | |
| | | `toolUseResult.totalTokens = 99999` | não conta (resumo do subagente) | | | | | | |
| `3333…/subagents/agent-a1` | subagent (associada) | cópia de `msg_FIX_B` | conta só na principal | | | | | | |
| | | `msg_FIX_D` | 1 | 200 | 5.000 | 800 | — | 6.001 | complete |
| `3333…/subagents/agent-a2` | subagent (associada) | `msg_FIX_E` | 4 | 120 | 3.000 | 0 | — | 3.124 | complete |
| `4444….jsonl` (`entrypoint: sdk-cli`) | automatic | `msg_FIX_F` | 6 | 90 | 2.000 | 500 | — | 2.596 | complete |
| `5555…/subagents/agent-o1` (pai ausente) | subagent **não associada** | `msg_FIX_G` | 2 | 60 | 1.500 | 100 | — | 1.662 | complete |

Totais Claude (7 registros após deduplicação): entrada 20 · saída 1.005 · cache lido 131.404 · cache criado 11.048 (6 de 7) · total 142.424 (6 de 7) · reasoning indisponível.

Sessão principal consolidada (principal + a1 + a2): total 138.166 (4 de 5 registros) = main_agent 129.041 + subagent 9.125.

## Codex (`codex/2026/11/20/`)

| Pedido | entrada (derivada = input − cached) | saída | cache lido | reasoning (dentro da saída) | total | acumulado |
|---|---|---|---|---|---|---|
| 1 | 1.051 | 316 | 7.808 | 70 | 9.175 | 9.175 |
| 2 | 4.453 | 308 | 9.088 | 46 | 13.849 | 23.024 |
| 2 (repetido, mesmo acumulado) | ignorado | | | | | |
| 3 | 10.985 | 440 | 13.568 | 243 | 24.993 | 48.017 |

`token_count` com `info: null`: ignorado. Soma dos pedidos 48.017 = acumulado final (reconciliado). Cache criado: indisponível. Modelo: `gpt-5.4`.
`rollout-…-7777….jsonl`: sessão sem nenhum `token_count` com `info` → 0 registros, consumo indisponível.

## OpenCode (`opencode/usage.sql`)

| Resposta | entrada | saída | cache lido | cache criado | reasoning | total | status |
|---|---|---|---|---|---|---|---|
| `msg_usage00002` | 11.440 | 286 | 0 | 0 | 0 | 11.726 (informado) | complete |
| `msg_usage00003` (sem total, reasoning 0) | 5.059 | 406 | 100 | 50 | 0 | 5.615 (derivado) | complete |
| `msg_usage00004` (sem total, reasoning 12) | 300 | 80 | 0 | 0 | 12 | — | partial |

A parte `step-finish` (total 5.465) não é contabilizada.
