# Analytics (Fase 8) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Seção Analytics (Overview, Usage, Activity) com consumo de tokens deduplicado por provider e atividade idêntica à do Dashboard.

**Architecture:** Adaptadores emitem `domain.Usage` (um por resposta/pedido) por uma interface opcional do sink; o indexador grava na tabela `usage` (migration 0004, releitura completa). `AnalyticsService` agrega em SQL (`usage_store.go`) e reaproveita `stats_store.go` para atividade; janela de período compartilhada com o Dashboard. Frontend com rotas `/analytics/*`, filtros na URL e gráficos SVG próprios.

**Tech Stack:** Go, modernc SQLite (window functions), Wails v3 bindings, React + TS, Tailwind (tema Aurora).

**Spec:** `docs/superpowers/specs/2026-11-22-analytics-design.md`

## Global Constraints

- Campo ausente = `NULL`/`null`, nunca `0`; agregados devolvem soma **e** cobertura.
- Reasoning nunca entra no total.
- Consumo inclui principal, subagente e automática; atividade usa `visibleS` (só principais não automáticas), sem controle para mudar.
- Texto obrigatório nas três telas: "Consumo de tokens inclui subagentes e sessões automáticas. Atividade considera somente sessões principais e não automáticas."
- Fuso: local do sistema (`time.Now().Zone()`), limites em meia-noite local.
- Sem custos, sem Control Center, sem rede, sem dependência nova, sem estimar tokens por texto.
- Session Manager (listas, viewer, busca, retomada, Dashboard) não muda de comportamento.
- Nunca ler `.credentials.json`, `auth.json` ou tabelas `account*`; nunca criar `-shm` em diretório de provider.
- Logs sem conteúdo de conversa. Commits só quando o usuário pedir.

## Review Focus

1. **Resposta do Claude com linhas não contíguas** (outra linha entre dois pedaços do mesmo `message.id`): continua 1 registro. Teste no Task 3 (`TestUsageNonContiguousLines`).
2. **Arquivo do Claude que termina no meio de uma resposta** (escrita em andamento): o registro da parte completa entra; a releitura posterior não duplica. Teste no Task 6 (`TestReindexGrowingFileNoDuplicate`).
3. **Codex sem nenhum `token_count` com `info`** (sessões antigas): sessão `unavailable`, sem registros e sem erro. Teste no Task 4 (`TestCodexNoUsage`).
4. **Filtro de projeto + provider que zera tudo**: Overview/Usage/Activity devolvem cobertura 0 → "indisponível"/vazio, sem `NaN`. Teste no Task 9 (`TestAnalyticsEmptySelection`).
5. **"Hoje" logo após meia-noite** e **mês anterior em janeiro**: janelas corretas. Teste no Task 7 (`TestWindowTodayAfterMidnight`, `TestWindowPrevMonthJanuary`).

---

### Task 1: Domínio, contrato e tabela `usage`

**Files:**
- Create: `internal/domain/usage.go`, `internal/domain/usage_test.go`, `internal/repository/sqlite/migrations/index/0004_usage.sql`
- Modify: `internal/provider/provider.go` (interface `UsageSink`), `internal/repository/sqlite/index_store.go` (`AddUsage`), `internal/indexer/indexer.go` (sink repassa), `internal/repository/sqlite/db_test.go`

**Interfaces:**
- Produces:
  ```go
  // domain
  type UsageStatus string // "complete" | "partial"
  type Usage struct {
      ResponseID string; Seq int; Timestamp *time.Time; Model string
      Input, Output, CacheRead, CacheCreation, Reasoning, Total *int64
      Derived []string    // nomes: "input", "total"
      Status  UsageStatus // preenchido pelo adaptador com StatusFor
  }
  func StatusFor(u Usage, expected []string) UsageStatus // complete se todos os campos de expected != nil
  // provider
  type UsageSink interface { Usage(key domain.SessionKey, u domain.Usage) error }
  // sqlite
  func (t *SourceTx) AddUsage(ctx context.Context, key domain.SessionKey, u domain.Usage) error
  ```
- Nomes de campo usados em `expected`/`Derived`: `input`, `output`, `cache_read`, `cache_creation`, `reasoning`, `total`.

- [ ] **Step 1: testes que falham**
  - `TestStatusFor`: expected `{input,output}` com Output nil → `partial`; ambos presentes → `complete`; Reasoning nil fora de expected não afeta.
  - `TestMigration0004ForcesReindex` (db_test): depois de abrir, `user_version = 4`, tabela `usage` existe com colunas `input_tokens … total_tokens` anuláveis, `sync_state` vazio.
  - `TestAddUsageNullsAndIdempotent` (index_store_test): `AddUsage` com Reasoning nil grava `NULL`; segundo `AddUsage` com mesmo `response_id` na mesma sessão é ignorado (1 linha); `derived` gravado como `"input,total"`; apagar a fonte (`BeginSource` de novo) remove os registros (cascata).
- [ ] **Step 2:** `go test ./internal/domain/ ./internal/repository/sqlite/ -run 'StatusFor|Migration0004|AddUsage'` → FAIL.
- [ ] **Step 3:** migration conforme spec §4.2 (insert com `ON CONFLICT (session_id, response_id) DO NOTHING`; índices `usage_ts_idx (ts, session_id)` e `usage_resp_idx (response_id)`; `DELETE FROM sync_state`). O sink do indexador implementa `UsageSink` chamando `tx.AddUsage(ctx, key, u)`, que grava `u.Status`.
- [ ] **Step 4:** mesmo comando → PASS; `go test ./...` verde.

### Task 2: Fixtures anonimizadas

**Files:**
- Create: `docs/discovery/fixtures/usage/README.md`, `claude/<main>.jsonl`, `claude/<main>/subagents/agent-a1.jsonl`, `…/agent-a2.jsonl`, `claude/<auto>.jsonl`, `claude/<orphan-parent-missing>/subagents/agent-o1.jsonl`, `codex/rollout-….jsonl`, `codex/rollout-old-….jsonl`, `opencode/usage.sql`

Origem: trechos reais dos históricos locais, com **estrutura e números de usage preservados** e texto, caminhos, IDs e UUIDs trocados por valores fixos (`/home/user/projects/demo-app`, `msg_FIX…`). O README lista, por arquivo, os valores esperados que os testes usam.

Conteúdo obrigatório:
- Claude principal: resposta `msg_FIX_A` em 3 linhas (uma não contígua, com uma linha `user` de tool_result entre elas), `usage` {input 2, output 150, cache_read 240000, cache_creation 1200}; resposta `msg_FIX_B` em 1 linha; uma linha `user` com `toolUseResult.totalTokens = 99999` (resumo do subagente, não conta); uma resposta `<synthetic>` com usage zerado (descartada).
- Subagente a1: resposta própria + **cópia de `msg_FIX_B`** (mesmo id, mesma usage); subagente a2: uma resposta.
- Automática: sessão sem prompt humano (entrypoint `sdk-cli`), uma resposta.
- Órfão: diretório de sessão cujo `.jsonl` principal não existe, com um subagente de uma resposta.
- Codex: `turn_context` gpt-5.4; 4 `token_count` com `last`/`total` reais encadeados, sendo o 3º repetido (mesmo total); `reasoning_output_tokens` > 0; um `info: null`. Codex old: nenhum `token_count` com info.
- OpenCode: 3 respostas — total informado; total ausente com reasoning 0; total ausente com reasoning 12; mais `step-finish` com tokens.

- [ ] **Step 1:** gerar com um script descartável no scratchpad (não versionado) e conferir à mão que nenhum texto real ficou (`grep -r` por caminhos de `/home/cesar` e por palavras do conteúdo original deve dar vazio).
- [ ] **Step 2:** README com a tabela de esperados (a mesma usada na tabela final do relatório).

### Task 3: Claude — extração de usage

**Files:** Modify `internal/provider/claude/claude.go`; Test `internal/provider/claude/usage_test.go`

**Interfaces:** Consumes `domain.Usage`, `provider.UsageSink`. Expected Claude = `{input, output, cache_read, cache_creation}`; Derived `{total}`.

- [ ] **Step 1: testes** (sink de teste que implementa `UsageSink`):
  - `TestUsageDedupByMessageID`: fixture principal → 2 registros (`msg_FIX_A`, `msg_FIX_B`); A: Input 2, Output 150, CacheRead 240000, CacheCreation 1200, Total 241352 derivado, Reasoning nil, status complete.
  - `TestUsageNonContiguousLines`: A continua 1 registro mesmo com linha intermediária.
  - `TestUsageIgnoresToolUseResultAndSynthetic`: nenhum registro com 99999; nenhum de `<synthetic>`.
  - `TestUsageMissingField`: linha com `cache_creation_input_tokens` ausente → CacheCreation nil, Total nil (não deriva com componente faltando), status partial.
- [ ] **Step 2:** FAIL.
- [ ] **Step 3:** `rawMessage` ganha `Usage *rawUsage` e `rawLine` ganha `RequestID`. Mapa por arquivo `responseID → (seq da 1ª linha, ts da 1ª linha, model, usage da última linha)`; emitir em ordem de seq após o loop, antes de `sink.Session`, só se `sink` implementar `UsageSink`. Identidade: `message.id` → `requestId` → `uuid`. Divergência de valores entre linhas → `stats` conta (novo campo `UsageConflicts int` em `ParseStats`, logado pelo indexador).
- [ ] **Step 4:** PASS; `go test ./internal/provider/...` verde.

### Task 4: Codex — extração de usage

**Files:** Modify `internal/provider/codex/codex.go`; Test `internal/provider/codex/usage_test.go`

Expected Codex = `{input, output, cache_read, reasoning, total}`; Derived `{input}` (= `input_tokens − cached_input_tokens`).

- [ ] **Step 1: testes**
  - `TestCodexUsageFromLastTokenUsage`: 3 registros (o repetido some); valores = os `last` da fixture; `Total` informado; `Input` derivado; Reasoning presente e **não** somado (Total = input_tokens + output_tokens do `last`).
  - `TestCodexReconciles`: soma dos `Total` = último `total_token_usage.total_tokens`; `ParseStats.UsageMismatch == false`.
  - `TestCodexCumulativeDecrease`: total que diminui é aceito e vira nova referência.
  - `TestCodexNoUsage`: fixture old → 0 registros, sem erro.
  - `TestCodexModelFromTurnContext`: Model = `gpt-5.4`.
- [ ] **Step 2:** FAIL.
- [ ] **Step 3:** identidade `fmt.Sprintf("%s#%d", sessionID, total)`; `ParseStats.UsageMismatch bool` quando a soma não bate (logado pelo indexador como aviso, sem conteúdo).
- [ ] **Step 4:** PASS.

### Task 5: OpenCode — extração de usage

**Files:** Modify `internal/provider/opencode/opencode.go`; Test `internal/provider/opencode/usage_test.go`

Expected = `{input, output, cache_read, cache_creation, total}` (reasoning informado mas fora de expected por ser categoria à parte). Abrir o banco como hoje (sem `-shm`).

- [ ] **Step 1: testes**
  - `TestOpenCodeUsagePerMessage`: 3 registros; `step-finish` ignorado; 1º total informado; 2º total derivado (`input+output+read+write`, em Derived); 3º Total nil, Reasoning 12, status partial.
  - `TestOpenCodeReasoningNotAdded`: no 1º registro, Total = valor informado (não += reasoning).
- [ ] **Step 2–4:** FAIL → implementar a partir do JSON `data` da mensagem já lido em `parseSession` → PASS.

### Task 6: Indexador — releitura idempotente e recuperação

**Files:** Modify `internal/indexer/indexer.go` (log de contagem de usage, `UsageConflicts`, `UsageMismatch`); Test `internal/indexer/usage_test.go`

- [ ] **Step 1: testes** (adaptadores reais sobre cópia das fixtures num `t.TempDir()`):
  - `TestReindexIdempotent`: indexar, contar `usage`; forçar releitura (`DELETE FROM sync_state`) e indexar de novo → mesma contagem e mesmas somas.
  - `TestReindexRecoversAfterFailure`: sink que falha no meio da fonte → nenhuma linha da fonte gravada, `sync_state` com erro; próxima execução grava tudo.
  - `TestReindexGrowingFileNoDuplicate`: arquivo truncado no meio de uma linha da resposta B → indexa A; completar o arquivo → releitura tem A e B uma vez cada.
- [ ] **Step 2–4:** FAIL → ajustes → PASS. Log por fonte ganha `usage=N` e avisos `usage_conflicts`/`usage_mismatch` (sem conteúdo).

### Task 7: Janela de período compartilhada

**Files:** Create `internal/service/period.go`, `internal/service/period_test.go`; Modify `internal/service/stats.go` (usar o helper), `internal/repository/sqlite/stats_store.go` (`StatsFilter.Project string`, `BucketHour`)

**Interfaces:**
```go
type Window struct {
    From, To, PrevFrom, PrevTo time.Time // PrevFrom zero = sem comparação
    Bucket sqlite.Bucket; Keys []string; Starts []time.Time; PrevKeys []string
    Offset int; Now time.Time
}
// period: today|7d|30d|90d|month|prev_month|custom|all; from/to "AAAA-MM-DD" só em custom
func resolveWindow(now time.Time, period, from, to string, first func() (time.Time, error)) (Window, error)
```
Regras: `today` usa `BucketHour` (chave `AAAA-MM-DDTHH`); demais conforme spec §6 (dia ≤ 92 dias, semana ≤ 2 anos, mês acima). `PrevTo` dos totais = mesmo tempo decorrido (regra atual do Dashboard); `prev_month` compara com o mês inteiro anterior; `all` sem anterior. `custom` inválido (from > to, formato) → `ErrInvalidFilter`.

- [ ] **Step 1: testes**: `TestWindowPeriods` (7d/30d/90d = Dashboard atual), `TestWindowTodayAfterMidnight` (now 00:10 → 1 intervalo-hora com chave do dia certo), `TestWindowThisMonth`, `TestWindowPrevMonthJanuary` (now 2027-01-15 → dez/2026 inteiro, anterior nov/2026), `TestWindowCustom` (limites meia-noite local, inclusivo no último dia), `TestWindowCustomInvalid`, `TestActivityProjectFilter` (stats_store: filtro de projeto restringe Activity e StatsTotals).
- [ ] **Step 2:** FAIL.
- [ ] **Step 3:** implementar; `StatsService.window` vira chamada a `resolveWindow` — **todos os testes atuais do Dashboard continuam passando sem mudança**.
- [ ] **Step 4:** `go test ./internal/service/ ./internal/repository/sqlite/` → PASS.

### Task 8: Consultas de consumo (`usage_store.go`)

**Files:** Create `internal/repository/sqlite/usage_store.go`, `usage_store_test.go`

**Interfaces:**
```go
type UsageFilter struct { From, To time.Time; Providers []domain.ProviderID; Project, Model string; Bucket Bucket; Offset int; NoTime bool /* "all": inclui ts NULL nos totais */ }
type Field struct { Sum int64 `json:"sum"`; Count int `json:"count"` }        // Count = registros com o campo
type TokenSums struct { Records int `json:"records"`; Input, Output, CacheRead, CacheCreation, Reasoning, Total Field; Derived int `json:"derived"` /* registros com total derivado */ }
type UsageGroup struct { Key, Label string; Tokens TokenSums }
type UsageDim string // "provider" | "model" | "project" | "origin"
type UsagePoint struct { Bucket string; Provider domain.ProviderID; Total Field }
type UsageSessionRow struct { SessionID int64; Key domain.SessionKey; Title, Project string; Origin string; Associated bool; Tokens TokenSums; FirstTs, LastTs *int64 }
type OriginShare struct { Origin string; Sessions int; Tokens TokenSums }
func (s *IndexStore) UsageTotals(ctx, f UsageFilter) (TokenSums, error)
func (s *IndexStore) UsageBy(ctx, f UsageFilter, dim UsageDim, limit int) ([]UsageGroup, error)
func (s *IndexStore) UsageSeries(ctx, f UsageFilter) ([]UsagePoint, error)
func (s *IndexStore) UsageSessions(ctx, f UsageFilter, offset, limit int) ([]UsageSessionRow, int, error) // linhas raiz, total de raízes
func (s *IndexStore) UsageSessionOrigins(ctx, f UsageFilter, rootID int64) ([]OriginShare, error)
func (s *IndexStore) UsageModels(ctx) ([]string, error)
```
Base de toda consulta (CTE `u`), sem `visibleS`:
```sql
WITH ranked AS (
  SELECT u.*, s.provider, s.project_id, s.parent_native_id, s.automated,
    ROW_NUMBER() OVER (PARTITION BY s.provider, u.response_id
      ORDER BY (s.parent_native_id IS NOT NULL), u.ts, u.session_id) AS rn
  FROM usage u JOIN sessions s ON s.id = u.session_id)
SELECT … FROM ranked WHERE rn = 1 AND <filtros>
```
Origem: `subagent` se `parent_native_id` não nulo; senão `automatic` se `automated = 1`; senão `main_agent`. Raiz: sessão pai `(provider, parent_native_id)` se existir no índice (`Associated = true`), senão a própria. Somas com `SUM(col)` e `COUNT(col)` (ignoram NULL).

- [ ] **Step 1: testes** (índice montado com as fixtures via indexador real, como no Task 6):
  - `TestUsageTotalsAllOrigins`: Records e somas = soma de todas as sessões das fixtures (principal + a1 + a2 + automática + órfão), `msg_FIX_B` uma vez.
  - `TestUsageCrossFileDedupPrefersMain`: `msg_FIX_B` atribuída à sessão principal.
  - `TestUsageNullNotZero`: Reasoning do Claude → `Count 0`, `Sum 0`, e o chamador distingue por Count; CacheCreation com Count < Records na fixture parcial.
  - `TestUsageByProviderModelProject`: grupos e somas esperados do README.
  - `TestUsageSessionsRollup`: linha da principal = principal + a1 + a2 (sem a cópia de B duplicada); automática e órfão em linhas próprias; órfão com `Associated=false`, Origin `subagent`.
  - `TestUsageOriginsReconcile`: soma de `UsageSessionOrigins` da principal = linha da principal; `UsageBy(origin)` soma = `UsageTotals`.
  - `TestUsageSeriesLocalDay`: registro 23:30 -03:00 cai no dia local.
- [ ] **Step 2–4:** FAIL → implementar → PASS. Medir `UsageTotals`/`UsageBy`/`UsageSessions` no índice real (só leitura, cache quente) e registrar no ledger; meta < 150 ms.

### Task 9: `AnalyticsService` e regra de domínio

**Files:** Create `internal/service/analytics.go`, `analytics_test.go`; Modify `app.go` (registrar), bindings (`make bindings`), `frontend/src/lib/api.ts`

**Interfaces:**
```go
type AnalyticsQuery struct { Period, From, To string; Providers []domain.ProviderID; Project, Model string }
type Availability struct { Field, Provider, Reason string } // ex.: reasoning/claude: "O Claude Code não informa reasoning separado."
type Overview struct { Window WindowInfo; Totals, Prev sqlite.TokenSums; HasPrev bool; ByProvider []sqlite.UsageGroup; Series []SeriesPoint /* total por provider, Field */; TopProjects []sqlite.UsageGroup; Unavailable []Availability }
type UsageView struct { Window WindowInfo; Totals sqlite.TokenSums; ByProvider, ByModel, ByProject, ByOrigin []sqlite.UsageGroup; Unavailable []Availability }
type ActivityView struct { Window WindowInfo; Sessions, Messages Metric; Series []SeriesPoint; ByProvider []ProviderShare; ByProject []sqlite.NamedCount; Heatmap [7][24]int /* seg=0, hora local */; ActiveDays, Days int; SessionsPerActiveDay float64 }
type WindowInfo struct { Period string; From, To int64; Bucket string }
func (a *AnalyticsService) Overview(ctx, q AnalyticsQuery) (Overview, error)
func (a *AnalyticsService) Usage(ctx, q AnalyticsQuery) (UsageView, error)
func (a *AnalyticsService) UsageSessions(ctx, q AnalyticsQuery, offset int) (UsageSessionPage, error) // 50 por página
func (a *AnalyticsService) SessionOrigins(ctx, q AnalyticsQuery, rootID int64) ([]sqlite.OriginShare, error)
func (a *AnalyticsService) Activity(ctx, q AnalyticsQuery) (ActivityView, error)
func (a *AnalyticsService) Models(ctx) ([]string, error)
```
`Activity` usa `stats_store` (`Activity`, `StatsTotals`, `TopProjects` por sessões…) com o mesmo `StatsFilter` e `visibleS`; heatmap e dias ativos são consultas novas em `stats_store.go` com o mesmo `where`. Overview e Activity ignoram `Model`. Provider inválido / custom inválido → `ErrInvalidFilter`. `Unavailable` lista, para os providers no recorte, os campos indisponíveis por natureza (tabela §2 do spec).

- [ ] **Step 1: testes**
  - `TestDomainRuleConsumptionIncludesAllOrigins` (a), `TestDomainRuleMainConsolidatesDescendants` (b), `TestDomainRuleOriginsReconcile` (c), `TestDomainRuleUnassociatedCounted` (d), `TestDomainRuleActivityExcludesHidden` (e), `TestActivityMatchesDashboard` (f: para 7d/30d/90d/all e filtro de provider, `Activity.Sessions/Messages` = `StatsService.Dashboard` e séries iguais ponto a ponto).
  - `TestAnalyticsUnavailableNotZero`: só Claude no recorte → `Unavailable` contém reasoning/claude; Totals.Reasoning.Count = 0.
  - `TestAnalyticsEmptySelection`: projeto inexistente → Records 0, séries com 0 por intervalo, sem erro.
  - `TestActivityHeatmapLocal`: mensagem segunda 23:30 -03:00 → `[0][23]`.
- [ ] **Step 2–4:** FAIL → implementar → PASS; `make bindings`; `npx tsc --noEmit`.

### Task 10: Frontend — seção, filtros e Overview

**Files:** Create `frontend/src/features/analytics/{AnalyticsLayout,AnalyticsFilters,OverviewPage,useAnalyticsQuery,format}.tsx|ts`, `frontend/src/components/charts/StackedBar.tsx`; Modify `App.tsx` (rotas), `lib/nav.ts` (item "Analytics", ícone `ChartColumn`, atalho nenhum), `lib/api.ts`

- Filtros na URL: `?period=&from=&to=&p=&project=&model=` (model só na aba Usage); `useAnalyticsQuery()` devolve `AnalyticsQuery` e `update(patch)`; pílulas com `toggleProviders`/`providerActive`; período por `Segmented` (Hoje, 7 dias, 30 dias, 90 dias, Este mês, Mês anterior, Tudo, Personalizado) e dois `<input type="date">` quando personalizado; projeto em `NativeSelect` (de `SessionService.Projects`).
- Texto de transparência (Global Constraints) sob os filtros, nas três abas.
- Overview: cartões de total, entrada, saída, cache lido, cache criado (com variação contra o anterior quando `HasPrev`); `AreaChart` da série por provider (unidade "tokens"); `Donut` por provider; `BarList` top projetos; `StackedBar` composição entrada/saída/cache.
- `format.ts`: `formatTokens(n)` ("1,2 mi", "350 mil", exato na dica); `fieldValue(field: Field, records: number)` → `{kind: "value"|"partial"|"unavailable", text, coverage}`.
- Cada card: skeleton, vazio ("Sem consumo registrado no período"), erro com "Tentar de novo".
- [ ] Verificação: `npx tsc --noEmit`; captura com `packaging/scripts/demo-home.py` (Task 12 estende o gerador com usage) e com índice vazio (sem `NaN`, campos indisponíveis como "indisponível").

### Task 11: Frontend — Usage e Activity

**Files:** Create `frontend/src/features/analytics/{UsagePage,SessionsUsageTable,ActivityPage}.tsx`, `frontend/src/components/charts/Heatmap.tsx`

- Usage: filtro de modelo (de `Models()`); por provider, por modelo, por projeto (`BarList` com tokens), cache (lido/criado e % sobre entrada total quando ambos têm cobertura), reasoning (só providers que informam; demais citados em `Unavailable`), por origem (principal/subagente/automática), tabela de sessões paginada (título, projeto, origem, "não associada", tokens, duração = `LastTs − FirstTs` ou "—"), expandir linha → `SessionOrigins`.
- Activity: sessões por intervalo (`AreaChart`, unidade "sessões"), por provider, por projeto, `Heatmap` 7×24 (dias seg–dom, horas 0–23 locais, legenda "mensagens"), frequência (dias ativos de N, sessões por dia ativo), comparação com anterior.
- Tooltips e legendas com unidade; estados de loading/vazio/erro em todos os cards.
- [ ] Verificação: `npx tsc --noEmit`; capturas das três abas com dados de exemplo e reais; checar que números de Activity batem com o Dashboard na mesma janela (30 dias).

### Task 12: Documentação, dados de exemplo e fechamento

**Files:** Create `docs/ANALYTICS.md`; Modify `docs/DOMAIN.md`, `docs/ARCHITECTURE.md`, `docs/ROADMAP.md`, `docs/PROVIDERS.md`, `CHANGELOG.md`, `packaging/scripts/demo-home.py` (usage sintético coerente por provider)

- `ANALYTICS.md`: métricas, unidades, períodos e fuso, regras por provider (spec §2–3), **regra de domínio consumo × atividade** (texto do usuário), qualidade (complete/partial/unavailable, derivado), limitações (Codex sem vínculo de pai; reasoning do OpenCode não verificável localmente; Claude sem reasoning; Codex sem cache_creation).
- ROADMAP: pendências para a Fase 9 (custos, vínculo de pai do Codex, filtro por data nas listas).
- [ ] `make check`, `make build`, `make vuln` verdes.
- [ ] Tabela final **esperado × calculado** para cada caso das fixtures (README do Task 2), gerada rodando os testes e um pequeno programa de teste que imprime os valores (`go test -run TestPrintVerificationTable -v ./internal/service/`).
- [ ] Revisão final do branch inteiro por revisor novo; uma rodada de correções com TDD.
