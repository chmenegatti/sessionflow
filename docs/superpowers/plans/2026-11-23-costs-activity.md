# Fase 9 — Custos e padrões de atividade: plano de implementação

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** estimativa de custo por resposta com tabela de preços local, versionada e com vigência; relatórios de custo; padrões de atividade; exportação CSV.

**Architecture:**
- **Cálculo:** puro, em `internal/domain/cost.go`.
- **Preços e estimativas:** gravados no `user.db` (migration `0002_pricing`).
- **Relatórios de custo:** rodam no `index.db` com `ATTACH` do `user.db` somente leitura, reusando a CTE de deduplicação da Fase 8.
- **Padrões:** extraem fatos por sessão em SQL e calculam os indicadores em Go.
- **Serviços:** `CostService` e `PatternsService`, ligados em `app.go`; `Refresh` roda depois de cada sync e depois de cada mudança de preço.

**Tech Stack:** Go 1.26, modernc SQLite, Wails v3, React 19 + TypeScript, Vitest (novo, só dev).

**Spec:** `docs/superpowers/specs/2026-11-23-costs-activity-design.md` (aprovada em 2026-11-23). Ler inteira antes da Task 1.

## Global Constraints

- **Sem commits:** regra do usuário. Cada task termina com `make check` verde e linha no ledger.
- **Fase:** não avançar para a Fase 10. Não implementar recursos do Control Center.
- **Proibido:** cobrança real, API de billing, consulta de preço externa, conversão cambial e LLM.
- **Dinheiro:** nunca `float64` para armazenar ou somar. Valor em nanos da moeda (`int64`); conta com `math/big`; arredondamento *half-even*.
- **Preço:** texto decimal canônico, ≥ 0, até 6 casas, sem expoente, ≤ 1.000.000. `NULL` = categoria sem preço; `0` = gratuito.
- **Unidade:** `unit_tokens` ∈ {1, 1000, 1000000}.
- **Moeda:** `^[A-Z]{3}$`; o serviço normaliza para maiúsculas. Moeda padrão BRL, no `config.json` (`defaultCurrency`).
- **Moedas nunca se somam;** todo agregado é por moeda.
- **Copy fixa (pt-BR):**
  - selo "Estimativa";
  - aviso de custos: "Calculado com os preços que você cadastrou. Não é o valor cobrado; planos por assinatura não são cobrados por token.";
  - aviso de padrões: "Métricas de atividade observada nos históricos. Não medem produtividade, qualidade, esforço ou valor entregue.";
  - rótulo da duração: "tempo entre a primeira e a última mensagem, não tempo de trabalho".
- **Status:** `complete | partial | no_price | insufficient`. Os motivos (`reasons`) são textos fixos, sem conteúdo de conversa.
- **Retomada:** pausa ≥ 30 min (constante `resumeGap = 30 * time.Minute`).
- **Logs:** sem conteúdo de conversa. Histórico do provider nunca é alterado. Processos sem shell.
- **Contratos da Fase 8 não mudam** (`AnalyticsQuery`, `Window`, `UsageFilter`, telas existentes).
- **Bindings:** depois de mudar serviço exportado, rodar `wails3 task common:generate:bindings`.

## Review Focus

1. **Mesmo modelo com preço em USD num período e em BRL em outro:** os totais saem em duas moedas e nada soma as duas. Teste `TestCostsNeverMixCurrencies` (Task 4).
2. **Usuário edita um preço enquanto a sync roda o `Refresh`:** nenhuma estimativa fica com revisão substituída por corrida, e `-race` fica limpo. Teste `TestRefreshSerialized` (Task 4).
3. **Caminho do `user.db` com `?`, `#`, `%` ou espaço no `ATTACH`:** abre o arquivo certo. Teste `TestCostStoreAttachEscapedPath` (Task 3).
4. **Preço com prefixo `*` e preço exato do mesmo modelo, maiúsculas diferentes:** vence o exato; entre prefixos, vence o mais longo. Teste `TestMatchPricePrecedence` (Task 1).
5. **CSV com projeto `=cmd|...`, vírgula, aspas ou acento no caminho:** a célula escapa e é prefixada com `'`, e o UTF-8 é preservado. Teste `TestCSVEscaping` (Task 6).

---

### Task 1: Motor de custo (domínio)

**Files:**
- Create: `internal/domain/cost.go`, `internal/domain/cost_test.go`

**Interfaces:**
- Produces:
  ```go
  const (CostInput = "input"; CostOutput = "output"; CostCacheRead = "cache_read"; CostCacheCreation = "cache_creation")
  type Nanos int64                         // MarshalJSON → string decimal com 9 casas ("0.108000000"); String() idem
  func ParseDecimal(s string) (micros int64, canonical string, err error)
  type UsageTokens struct{ Input, Output, CacheRead, CacheCreation *int64 }
  type PriceRevision struct {
    ID int64; PriceKey string; Provider ProviderID; Model, Currency string; UnitTokens int64
    Input, Output, CacheRead, CacheCreation *string          // decimal canônico; nil = sem preço
    ValidFrom time.Time; ValidTo *time.Time; Source, Note string
    CreatedAt time.Time; ReplacedBy *int64; DisabledAt, ClosedAt *time.Time
  }
  func (r PriceRevision) Current() bool                     // ReplacedBy == nil && DisabledAt == nil
  type EstimateStatus string // EstimateComplete, EstimatePartial, EstimateNoPrice, EstimateInsufficient
  type Estimate struct {
    RevisionID *int64; Currency string; UnitTokens int64; Tokens UsageTokens
    InputCost, OutputCost, CacheReadCost, CacheCreationCost, Total *Nanos
    Status EstimateStatus; Reasons []string
  }
  func ExpectedCostCategories(p ProviderID) []string        // codex: sem cache_creation
  func ComputeEstimate(rev *PriceRevision, p ProviderID, model string, hasTs bool, u UsageTokens) Estimate
  func MatchPrice(revs []PriceRevision, p ProviderID, model string, ts time.Time) *PriceRevision
  var ErrInvalidPrice = errors.New("preço inválido")
  func ValidateRevision(r PriceRevision, others []PriceRevision) error // errors.Is(err, ErrInvalidPrice)
  ```
- **Motivos fixos:**
  - `"sem horário"`, `"sem modelo"`, `"sem tokens"`, `"sem preço vigente"`, `"valor fora do limite"`;
  - `"<rótulo> sem preço"` e `"<rótulo> indisponível"`, com os rótulos `entrada`, `saída`, `cache lido` e `cache criado`.

- [ ] **Step 1: Write the failing tests** in `cost_test.go`:
  - **`TestComputeEstimateFormula`.** Revisão com unidade 1.000.000 e preços entrada 3.00, saída 15.00, cache lido 0.30, cache criado 3.75. Tokens 1000 / 500 / 200000 / 10000 (Claude). Espera:

    | campo | nanos |
    |---|---|
    | entrada | 3.000.000 |
    | saída | 7.500.000 |
    | cache lido | 60.000.000 |
    | cache criado | 37.500.000 |
    | total | 108.000.000 |

    Também espera `Total.String() == "0.108000000"` e status `complete`.
  - **`TestComputeEstimateUnits`.** O mesmo custo com unidade 1000 (preço 0.003) e unidade 1 (preço 0.000003) dá os mesmos nanos.
  - **`TestComputeEstimateNoFloat`.** Unidade 1, preços 0.1 e 0.2, 1 token de entrada e 1 de saída → total exatamente 300.000.000.
  - **`TestComputeEstimateHalfEven`.** Unidade 1.000.000, preço de entrada 0.000001. Tokens 500 → 0; 1500 → 2; 2500 → 2; 3500 → 4.
  - **`TestComputeEstimateOverflow`.** Tokens 10¹², preço 1000000, unidade 1 → `insufficient` com motivo `"valor fora do limite"`, sem custo e sem panic.
  - **`TestComputeEstimateStatus`** (tabela):
    - Claude, cache criado sem preço e 10000 tokens → `partial`, `"cache criado sem preço"`, entrada calculada;
    - Claude, cache criado sem preço e 0 tokens → `complete`;
    - Codex, `CacheCreation` nil → `complete`;
    - Claude, `CacheCreation` nil com preço → `partial`, `"cache criado indisponível"`;
    - `rev == nil` com tokens → `no_price`, `"sem preço vigente"`, todos os custos nil;
    - `hasTs = false` → `insufficient`, `"sem horário"`;
    - modelo `""` → `insufficient`, `"sem modelo"`;
    - todos os tokens nil → `insufficient`, `"sem tokens"`;
    - preço `"0"` → custo 0 e `complete`.
  - **`TestMatchPricePrecedence`.**
    - exato `claude-opus-5-5` vence `claude-opus-*`; `claude-opus-*` vence `claude-*`;
    - o modelo `Claude-Opus-5-5` casa sem diferenciar maiúsculas;
    - outro provider não casa;
    - revisão substituída ou desativada não casa.
  - **`TestMatchPriceValidity`.** `ts == ValidFrom` casa; `ts == *ValidTo` não casa; `ts` antes de `ValidFrom` não casa.
  - **`TestValidateRevision`** (tabela de erros):
    - moeda `"usd"` e `"US"`;
    - unidade 100;
    - todos os preços nil;
    - preço `"1e3"`, `"-1"`, `"1.1234567"` e `"1000000.01"`;
    - `ValidTo <= ValidFrom`;
    - sobreposição com revisão atual do mesmo provider e modelo (sem diferenciar maiúsculas).

    Válidos: vigências adjacentes `[a,b)` e `[b,∞)`; sobreposição com revisão substituída.
  - **`TestParseDecimal`.** `"3"` → (3000000, `"3"`); `"3.50"` → (3500000, `"3.5"`); `"0.000001"` → (1, `"0.000001"`); `" 3"` → erro.
  - **`TestNanosJSON`.** `Nanos(5)` → `"0.000000005"`.
- [ ] **Step 2: Run** `go test ./internal/domain/ -run 'Cost|Price|Decimal|Nanos'`. Expected: FAIL (símbolos indefinidos).
- [ ] **Step 3: Implement.** Conta: `num = tokens × micros × 1000`, `q, r = num.QuoRem(unit)`, arredonda *half-even* com `2r` contra `unit`. Se o resultado ou a soma passar de `math.MaxInt64`, aplica o motivo de overflow. Uma categoria só é "indisponível" quando está em `ExpectedCostCategories` e os tokens são nil.
- [ ] **Step 4: Run** a mesma linha. Expected: PASS. Depois `make check`.

### Task 2: Migration e `PriceStore` (user.db)

**Files:**
- Create: `internal/repository/sqlite/migrations/user/0002_pricing.sql`, `internal/repository/sqlite/price_store.go`, `internal/repository/sqlite/price_store_test.go`

**Interfaces:**
- Consumes: Task 1 (`PriceRevision`, `Estimate`, `Nanos`).
- Produces:
  ```go
  type EstimateKey struct{ Provider domain.ProviderID; NativeID, ResponseID string }
  type StoredEstimate struct{ Key EstimateKey; domain.Estimate; UsageTs *int64; CalculatedAt time.Time }
  var ErrRevisionReplaced = errors.New("revisão já substituída")
  var ErrValidityClosed  = errors.New("vigência já encerrada")
  func NewPriceStore(db *sql.DB) *PriceStore
  func (s *PriceStore) Revisions(ctx) ([]domain.PriceRevision, error)            // todas, por price_key e created_at
  func (s *PriceStore) InsertRevision(ctx, r domain.PriceRevision) (int64, error)
  func (s *PriceStore) ReplaceRevision(ctx, oldID int64, r domain.PriceRevision) (int64, error) // tx
  func (s *PriceStore) CloseValidity(ctx, id int64, to, at time.Time) error
  func (s *PriceStore) Disable(ctx, id int64, at time.Time) error
  func (s *PriceStore) EstimateUses(ctx) (map[int64]int, error)
  func (s *PriceStore) Estimates(ctx) (map[EstimateKey]StoredEstimate, error)
  func (s *PriceStore) SaveEstimates(ctx, es []StoredEstimate) error             // upsert em uma tx
  ```
- **Esquema:** exatamente o da spec §3.1 e §4.1, mais `closed_at INTEGER`. Índices `price_revisions(provider, model)` e `cost_estimates(revision_id)`. `reasons` gravado com separador `;`.

- [ ] **Step 1: Write the failing tests:**
  - **`TestPriceStoreRoundTrip`:** todos os campos, inclusive nil.
  - **`TestReplaceRevisionTwiceFails`:** a segunda troca do mesmo `oldID` dá `ErrRevisionReplaced` e não insere linha.
  - **`TestCloseValidityOnce`:** a segunda chamada dá `ErrValidityClosed`; `closed_at` gravado.
  - **`TestSaveEstimatesUpsert`:** gravar duas vezes a mesma chave deixa uma linha, com os valores da segunda.
  - **`TestEstimateUses`:** conta por `revision_id`.
  - **`TestUserMigration0002`:** um `user.db` criado só com a 0001 (favorito gravado) migra e mantém o favorito.
- [ ] **Step 2: Run** `go test ./internal/repository/sqlite/ -run 'Price|Estimate|Migration0002'`. Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run.** Expected: PASS; `make check`.

### Task 3: `CostStore` (relatórios com ATTACH) e linhas para estimar

**Files:**
- Create: `internal/repository/sqlite/cost_store.go`, `internal/repository/sqlite/cost_store_test.go`
- Modify: `internal/repository/sqlite/usage_store.go`. Extrair os filtros de `base()` num helper `(f UsageFilter) conds()`, sem mudar o SQL gerado.

**Interfaces:**
- Consumes: `UsageFilter`, `base()` (Fase 8), `EstimateKey` (Task 2).
- Produces:
  ```go
  type UsageItem struct{ Key EstimateKey; Ts *int64; Model string; Tokens domain.UsageTokens }
  func (s *IndexStore) UsageForEstimate(ctx, f UsageFilter) ([]UsageItem, error) // SEM deduplicação; NoTime/From/To/Providers/Project/Model
  type CostSums struct {
    Currency string                     // "" = linhas sem custo (no_price/insufficient/sem estimativa)
    Input, Output, CacheRead, CacheCreation, Total domain.Nanos
    Records, Complete, Partial, NoPrice, Insufficient, Unestimated int
  }
  type CostRow struct{ Provider domain.ProviderID; Model, Project, ProjectName, Bucket string; Sums CostSums }
  func NewCostStore(index *sql.DB, userPath string) *CostStore
  func (c *CostStore) Breakdown(ctx, f UsageFilter, byBucket bool) ([]CostRow, error)
  type CostSessionRow struct{ SessionID int64; Key domain.SessionKey; Project string; Associated bool; Sums []CostSums }
  func (c *CostStore) Sessions(ctx, f UsageFilter, offset, limit int) ([]CostSessionRow, int, error)      // raiz, ordenado pelo maior total da 1ª moeda
  type UncalculableRow struct{ SessionID int64; Key domain.SessionKey; Project string; NoPrice, Insufficient, Unestimated int; Reasons []string }
  func (c *CostStore) Uncalculable(ctx, f UsageFilter, offset, limit int) ([]UncalculableRow, int, error)
  ```
- **`ATTACH`:**
  - roda em `index.Conn(ctx)` com `ATTACH DATABASE ? AS u`;
  - o argumento é `"file:" + (&url.URL{Path: userPath}).EscapedPath() + "?mode=ro"`, o mesmo escape de `dsn`;
  - `DETACH` e `conn.Close()` em `defer`.
  - Se a URI não abrir no ATTACH, usar o caminho puro e registrar `Ruling` no ledger. Só há SELECT.
- `Partial` também entra em `Currency` (tem custo). Linhas com custo nil vão para `Currency ""`.

- [ ] **Step 1: Write the failing tests** (ambiente `indexertest.NewUsageEnv` + `user.db` temporário + `PriceStore`):
  - **`TestUsageForEstimateIncludesCopies`:** conta igual a `SELECT count(*) FROM usage` e maior que `TokenSums.Records` da Fase 8 (a cópia do subagente aparece).
  - **`TestCostBreakdownDedup`:**
    - com preço Claude USD e todas as estimativas gravadas, a soma de `Total` por moeda do `Breakdown` é a soma das estimativas das linhas vencedoras;
    - a resposta copiada para o subagente conta uma vez;
    - `Records` igual a `TokenSums.Records`.
  - **`TestCostSessionsReconcile`:** a soma de `Sessions` por moeda é igual ao total do `Breakdown`.
  - **`TestCostUncalculable`:** sem preço para OpenCode, as sessões OpenCode aparecem com `NoPrice > 0` e motivo `"sem preço vigente"`; linhas sem estimativa contam em `Unestimated`.
  - **`TestCostStoreAttachEscapedPath`:** `user.db` num diretório `a?b#c%d e` funciona.
  - **`TestCostStoreDetaches`:** depois de 3 consultas, `PRAGMA database_list` numa conexão do pool não lista `u`.
- [ ] **Step 2: Run** `go test ./internal/repository/sqlite/ -run 'Cost|UsageForEstimate'`. Expected: FAIL.
- [ ] **Step 3: Implement.** O CTE `d` da Fase 8 faz `LEFT JOIN u.cost_estimates e` em (d.provider, s.native_id, d.response_id). Para isso, `d` passa a expor `native_id` (a coluna nova não muda os resultados da Fase 8).
- [ ] **Step 4: Run.** Expected: PASS. Rodar os testes da Fase 8: `go test ./internal/repository/sqlite/ ./internal/service/` verde.

### Task 4: `CostService` (refresh, recálculo, preços, relatórios, wiring)

**Files:**
- Create: `internal/service/cost.go`, `internal/service/cost_test.go`
- Modify:
  - `internal/config/config.go`: `DefaultCurrency string json:"defaultCurrency"`, padrão `"BRL"`, `Validate` com `^[A-Z]{3}$`;
  - `internal/config/config_test.go`;
  - `app.go`: wiring, evento `costs:updated`, encadear `Refresh` no notify da sync;
  - `frontend/src/lib/api.ts`: exportar os tipos e serviços novos.

**Interfaces:**
- Consumes: Tasks 1–3; `AnalyticsService.usageWindow` (mesmo pacote).
- Produces:
  ```go
  const EventCostsUpdated = "costs:updated"
  type CostDeps struct {
    Index *sqlite.IndexStore; Costs *sqlite.CostStore; Prices *sqlite.PriceStore
    Analytics *AnalyticsService; Config *ConfigStore; Log *slog.Logger
    Notify func()                              // emite costs:updated
    Save   func(defaultName string) (string, error) // diálogo nativo; "" = cancelado (Task 6)
  }
  func NewCostService(d CostDeps) *CostService       // campos internos: mu sync.Mutex; now; async func(func()) (padrão: go)
  func (c *CostService) Refresh(ctx) (RefreshReport, error)          // RefreshReport{Calculated, Updated int}
  func (c *CostService) PreviewRecalculate(ctx, q AnalyticsQuery) (RecalcPreview, error)
  func (c *CostService) Recalculate(ctx, q AnalyticsQuery) (RecalcPreview, error)
  type RecalcPreview struct{ Changes int; Before, After []sqlite.CostSums }
  func (c *CostService) Costs(ctx, q AnalyticsQuery, bucket string) (CostView, error) // bucket "" = automático (Window); "day"|"week"|"month" força o intervalo da série; outro → ErrInvalidFilter
  type CostView struct {
    Window WindowInfo; HasPrices bool
    Totals, Prev []sqlite.CostSums             // por moeda; Prev vazio se !HasPrev
    Quality sqlite.CostSums                    // contagens de status de todas as linhas (Currency "")
    ByProvider, ByModel, ByProject []CostGroup // CostGroup{Key, Label string; Sums []sqlite.CostSums}
    Series []CostPoint                         // CostPoint{Start int64; ByCurrency map[string]domain.Nanos; Records, Covered int}
  }
  func (c *CostService) CostSessions(ctx, q AnalyticsQuery, offset int) (CostSessionPage, error)       // 50 por página
  func (c *CostService) Uncalculable(ctx, q AnalyticsQuery, offset int) (UncalculablePage, error)
  type PriceInput struct {
    Provider domain.ProviderID; Model, Currency string; UnitTokens int64
    Input, Output, CacheRead, CacheCreation *string; ValidFrom, ValidTo string // "AAAA-MM-DD"; ValidTo "" = sem fim (exclusivo: dia seguinte à meia-noite local)
    Source, Note string
  }
  type PriceItem struct{ Current domain.PriceRevision; History []domain.PriceRevision; Uses map[int64]int }
  func (c *CostService) Prices(ctx) ([]PriceItem, error)
  func (c *CostService) AddPrice(ctx, in PriceInput) (domain.PriceRevision, error)
  func (c *CostService) EditPrice(ctx, id int64, in PriceInput) (domain.PriceRevision, error)
  func (c *CostService) NewPriceFrom(ctx, id int64, in PriceInput) (domain.PriceRevision, error) // fecha valid_to do id em in.ValidFrom; mesma tx lógica
  func (c *CostService) DisablePrice(ctx, id int64) error
  func (c *CostService) ReactivatePrice(ctx, id int64) (domain.PriceRevision, error) // nova revisão, cópia sem disabled
  func (c *CostService) DefaultCurrency() string
  func (c *CostService) SetDefaultCurrency(ctx, cur string) error
  ```
- **Regras do `Refresh`** (spec §4.4):
  - sem estimativa → calcula com `MatchPrice` no `ts`;
  - `no_price` ou `insufficient` → recalcula;
  - tokens diferentes da cópia → recalcula com a **revisão gravada**, buscada por id entre todas as revisões;
  - nos demais casos, não toca.
- **Mutex:** `Refresh`, `Recalculate` e o CRUD tomam `c.mu`. Depois de cada CRUD, `c.async(func(){ c.Refresh(bg); c.Notify() })`.
- **Erro de validação:** `ErrInvalidPrice` (Task 1) volta como está, para a UI exibir. Erro de banco passa por `fail` (padrão `AnalyticsService.fail`).
- **`app.go`:**
  - o notify da sync passa a `func(){ app.Event.Emit(EventIndexUpdated); go func(){ costSvc.Refresh(ctx); app.Event.Emit(EventCostsUpdated) }() }`;
  - registrar `application.RegisterEvent[application.Void](service.EventCostsUpdated)`;
  - `Save` = `app.Dialog.SaveFile().SetFilename(name).PromptForSingleSelection()`.

- [ ] **Step 0:** `TestCostsBucketOverride` — 90d com bucket "week" → série em semanas (segunda), soma da série = total; "month" → meses; "ano" → ErrInvalidFilter. Entra junto com os testes do Step 1.
- [ ] **Step 1: Write the failing tests** (`costEnv`: `analyticsEnv` + `user.db`, `async` síncrono, `now = statNow`):
  - **`TestCostEditPriceKeepsEstimates`:** `AddPrice` Claude USD → total T; `EditPrice` com o dobro dos preços → `Costs` ainda T, estimativas na revisão antiga.
  - **`TestCostRecalculateAppliesCurrent`:**
    - depois da edição, `PreviewRecalculate(allClaude)` dá `Changes > 0` e `After` igual a 2T;
    - `Recalculate` → `Costs` dá 2T;
    - uma segunda prévia dá `Changes == 0`.
  - **`TestCostNoPriceAutoPriced`:** `Refresh` sem preços → `Quality.NoPrice == Records`; `AddPrice` → `Quality.Complete > 0` sem chamar `Refresh` à mão.
  - **`TestCostGrowingTokensKeepRevision`:**
    - estimar, editar o preço e então rodar `UPDATE usage SET output_tokens = output_tokens + 1000` numa linha;
    - `Refresh` → aquela estimativa usa a revisão antiga e o custo de saída sobe 1000 × preço antigo.
  - **`TestCostDisabledRevisionStays`:** desativar o preço → as estimativas antigas mantêm `RevisionID` e custo; uma resposta nova sem preço vigente fica `no_price`.
  - **`TestCostCloseValidityKeepsEstimates`:** `NewPriceFrom` com data posterior → as estimativas anteriores não mudam; `Prices` mostra o fechamento sem nova revisão no histórico do preço anterior.
  - **`TestCostRecalculateIncludesDedupCopies`:** depois de `Recalculate`, a estimativa da cópia do subagente também aponta para a revisão nova.
  - **`TestCostsNeverMixCurrencies`:** Claude em USD e Codex em BRL → `Totals` com duas entradas, cada uma igual à soma das linhas daquela moeda.
  - **`TestCostsComparePrevious`:** período `custom` com dados nos dois lados → `Prev` preenchido pela regra `PrevTotals*`; com `all`, `Prev` vazio.
  - **`TestCostsWithoutPrices`:** `HasPrices == false`, `Totals` vazio e `Quality.NoPrice > 0`, sem erro.
  - **`TestRefreshSerialized`:** 4 goroutines alternando `Refresh`, `EditPrice` e `Recalculate`, com `-race`; no fim, nenhuma estimativa aponta para revisão inexistente.
  - **`TestPriceInputValidation`:** moeda `"usd"` é aceita e normalizada; `ValidTo` antes de `ValidFrom` → `ErrInvalidPrice`.
  - **`TestConfigDefaultCurrency`** (config): padrão `"BRL"`; `"real"` inválido.
- [ ] **Step 2: Run** `go test ./internal/service/ ./internal/config/ -run 'Cost|Price|Refresh|DefaultCurrency'`. Expected: FAIL.
- [ ] **Step 3: Implement.** Depois, regerar os bindings.
- [ ] **Step 4: Run.** Expected: PASS. `make check` e `make build`.
- [ ] **Step 5: Desempenho.**
  - Teste temporário `internal/service/zz_perf_test.go` sobre uma **cópia** de `~/.local/share/sessionflow/index.db` no scratchpad, com um `user.db` novo e um preço por modelo;
  - medir `Refresh` (meta < 300 ms) e `Costs`, `CostSessions` em 30d, 90d e all (meta < 150 ms);
  - anotar os números no ledger e apagar o teste e a cópia.

### Task 5: Padrões de atividade

**Files:**
- Create: `internal/repository/sqlite/pattern_store.go`, `internal/repository/sqlite/pattern_store_test.go`, `internal/service/patterns.go`, `internal/service/patterns_test.go`
- Modify: `app.go` (wiring), `frontend/src/lib/api.ts`

**Interfaces:**
- Consumes: `StatsFilter` e a regra de visibilidade do Dashboard (`stats_store.go`); `resolveWindow` e `AnalyticsService` (janela de atividade, igual a `Activity`).
- Produces:
  ```go
  type SessionFact struct {
    SessionID int64; Provider domain.ProviderID; Project, ProjectName string
    CreatedAt *int64; FirstTs, LastTs *int64       // primeira/última mensagem com texto (em todo o histórico da sessão)
    MessagesInPeriod int; ResumedAt *int64         // 1ª mensagem com texto no período após pausa ≥ gap desde a anterior com texto
  }
  func (s *IndexStore) SessionFacts(ctx, f StatsFilter, gap time.Duration) ([]SessionFact, error) // sessões visíveis com mensagem com texto no período
  type DayCount struct{ Day string; Sessions, Messages int } // dia local AAAA-MM-DD
  func (s *IndexStore) DailyActivity(ctx, f StatsFilter) ([]DayCount, error)
  func (s *IndexStore) LastActivityByProject(ctx, providers []domain.ProviderID, before time.Time) (map[string]int64, error)

  func NewPatternsService(index *sqlite.IndexStore, analytics *AnalyticsService, log *slog.Logger) *PatternsService
  func (p *PatternsService) Patterns(ctx, q AnalyticsQuery) (PatternsView, error)
  type Distribution struct{ Median, P90 int64; Buckets []BucketCount } // BucketCount{Label string; Count int}; nearest-rank
  type PatternsView struct {
    Window WindowInfo
    Started Metric; StartedSeries []SeriesPoint; PrevStarted []int
    Resumed Metric
    Frequency struct{ ActiveDays, ActiveWeeks, Days, CurrentStreak, LongestStreak int; SessionsPerActiveDay float64 }
    Calendar []sqlite.DayCount
    TopProjects []ProjectActivity  // {Path, Name string; Sessions, Messages, ActiveDays, DaysSinceLast int}; top 10
    RecurringProjects int
    Heatmap [7][24]int             // reusa IndexStore.ActivityHeatmap
    Duration, MessagesPerSession, Gaps Distribution
    MultiProvider []ProjectProviders // {Path, Name string; Providers []ProviderCount{Provider; Sessions int}}
    ByProvider []ProviderPattern     // {Provider; Sessions, Messages, ActiveDays int; MedianDuration int64}
  }
  ```
- **Faixas:**
  - duração (ms): `< 5 min`, `5–30 min`, `30 min–2 h`, `> 2 h`;
  - mensagens por sessão: `1–5`, `6–20`, `21–50`, `> 50`;
  - intervalo entre sessões: `< 1 h`, `1–24 h`, `1–7 dias`, `> 7 dias`.
- **Referência das sequências e de `DaysSinceLast`:** `min(now, w.To − 1ms)`, no dia local.
- **Calendário:** até 366 dias; um período maior usa os últimos 366 dias.
- `SessionsPerActiveDay` é `float64`. É contagem, não dinheiro.

- [ ] **Step 1: Write the failing tests** (ambiente `newStatsEnv`, `e.sess(...)`, mensagens com texto):
  - **`TestSessionFactsResumedGap`:** mensagens às 10:00 e 10:29 → `ResumedAt` nil; às 10:00 e 10:30 → `ResumedAt` igual a 10:30.
  - **`TestSessionFactsResumedAcrossPeriod`:** mensagem dia 21 às 23:00 e dia 22 às 08:00, período `today` em 22 → retomada (a anterior está fora do período).
  - **`TestPatternsStreaks`:**
    - dias ativos 18, 19, 20 e 22/11 com `now` = 22/11 → `CurrentStreak` 1 e `LongestStreak` 3;
    - com `prev_month` (outubro sem atividade) → os dois 0.
  - **`TestPatternsMultiProvider`:** Claude e Codex em `/p` e só Claude em `/q` → `MultiProvider` lista apenas `/p`, com os dois agentes.
  - **`TestPatternsDistributions`:** durações 1, 10, 40 e 200 min → mediana 10 min (nearest-rank), P90 200 min, faixas [1,1,1,1].
  - **`TestPatternsGaps`:** inícios às 10:00, 12:00 e 12:00 do dia seguinte → intervalos 2 h e 24 h, faixas `1–24 h` = 1 e `1–7 dias` = 1.
  - **`TestPatternsRecurring`:** projeto com sessões em 2 dias distintos conta; com 1 dia não conta.
  - **`TestPatternsParityWithActivity`:** para 7d, 30d e all, `Frequency.ActiveDays` igual a `Activity.ActiveDays` e `Heatmap` igual a `Activity.Heatmap`.
  - **`TestPatternsExcludeHidden`:** subagente e sessão automática não entram em nenhum indicador.
  - **`TestPatternsCopy`:** nenhum nome de campo JSON nem string do serviço contém "produtiv" ou "eficiên" (varrer a serialização).
- [ ] **Step 2: Run** `go test ./internal/repository/sqlite/ ./internal/service/ -run 'SessionFacts|Patterns'`. Expected: FAIL.
- [ ] **Step 3: Implement.**
  - `SessionFacts` usa `LAG(ts) OVER (PARTITION BY session_id ORDER BY ts)` sobre as mensagens com `has_text = 1` das sessões visíveis;
  - os agregados ficam em Go.
  - Medir na cópia do índice real, como no Step 5 da Task 4. Meta: < 150 ms em 90d.
- [ ] **Step 4: Run.** Expected: PASS; regerar bindings; `make check`.

### Task 6: Exportação CSV

**Files:**
- Create: `internal/service/export.go`, `internal/service/export_test.go`
- Modify: `internal/service/cost.go`, `internal/service/patterns.go` (métodos `ExportCSV`); `PatternsService` ganha o campo `save` (no construtor, parâmetro `save func(string) (string, error)`).

**Interfaces:**
- Consumes: `CostView`/`Breakdown` (Task 3/4) e `PatternsView` (Task 5).
- Produces:
  ```go
  var csvHeader = []string{"periodo_inicio","periodo_fim","agente","modelo","projeto","sessao","metrica","valor","moeda","status","cobertura"}
  type csvRow [11]string
  func writeCSV(w io.Writer, rows []csvRow) error         // encoding/csv, vírgula, CRLF não; UTF-8 sem BOM
  func csvText(s string) string                            // prefixa "'" se começar com = + - @ (só colunas de texto)
  func (c *CostService) ExportCSV(ctx, q AnalyticsQuery) (string, error)      // caminho salvo; "" se cancelado
  func (p *PatternsService) ExportCSV(ctx, q AnalyticsQuery) (string, error)
  ```
- **Linhas de custo:** uma por (agente × modelo × projeto × moeda) do `Breakdown`. As métricas são:
  - `custo_total`, `custo_entrada`, `custo_saida`, `custo_cache_lido` e `custo_cache_criado`, em `Nanos.String()`;
  - `respostas`, `respostas_completas`, `respostas_parciais`, `respostas_sem_preco` e `respostas_sem_dados`.

  `status` é o pior status do grupo; `cobertura` é a % de respostas com custo, com 1 casa e ponto decimal.
- **Linhas de padrões:** uma por indicador e grupo. Exemplos: `sessoes_iniciadas`, `sessoes_retomadas`, `dias_ativos`, `duracao_mediana_ms`, `mensagens_por_sessao_mediana`, `intervalo_mediano_ms` e, por projeto e agente, `sessoes` e `mensagens`. `moeda`, `status` e `cobertura` ficam vazios.
- **Datas:** ISO 8601 com o fuso local (`2026-11-01T00:00:00-03:00`).
- **Sessão:** só `provider:native_id`, nunca o título.
- **Nome padrão:** `sessionflow-custos-AAAA-MM-DD.csv` e `sessionflow-padroes-AAAA-MM-DD.csv`.
- **Arquivo:** gravado com `0o600`.

- [ ] **Step 1: Write the failing tests:**
  - **`TestCSVEscaping`:**
    - o projeto `/home/x,"y"/=cmd|ação` sai entre aspas, com as aspas dobradas, prefixo `'` e acento preservado;
    - o modelo `-x` vira `'-x`;
    - o valor `-5` na coluna `valor` não é prefixado.
  - **`TestCostCSVContent`:**
    - cabeçalho exato;
    - a linha `custo_total` do grupo Claude USD tem valor igual a `Nanos.String()` do `Breakdown`, com 9 casas;
    - moeda `USD`;
    - o CSV não contém nenhum título de sessão dos fixtures.
  - **`TestCostCSVCancelled`:** `Save` devolve `""` → retorna `""` e nil, sem arquivo.
  - **`TestPatternsCSVContent`:** existe a linha `sessoes_retomadas` com valor igual a `PatternsView.Resumed.Value`.
- [ ] **Step 2: Run** `go test ./internal/service/ -run 'CSV'`. Expected: FAIL.
- [ ] **Step 3: Implement.** Regerar bindings.
- [ ] **Step 4: Run.** Expected: PASS; `make check`.

### Task 7: Vitest e formatação de dinheiro

**Files:**
- Modify: `frontend/package.json` (devDependency `vitest`, script `"test": "vitest run"`), `Makefile` (`check` roda `cd frontend && npm test`)
- Create:
  - `frontend/src/lib/money.ts`, `frontend/src/lib/money.test.ts`;
  - `frontend/src/features/analytics/format.test.ts`, para os casos dos scripts avulsos da Fase 8: `centerTotal` 4 casos, `coverageNote`, `sharePct` e `toggleProviders`.

**Interfaces:**
- Produces:
  ```ts
  export function roundDecimal(s: string, places: number): string   // half-even sobre a string decimal; sem Number
  export function formatMoney(s: string, currency: string): string   // 2 casas; se 0 < |v| < 0,01, até 6 casas; Intl pt-BR
  export function moneyStatusLabel(status: "complete"|"partial"|"no_price"|"insufficient"): string
  ```
  Rótulos: "completa", "parcial", "sem preço" e "sem dados suficientes".

- [ ] **Step 1: Write the failing tests:**
  - `roundDecimal("0.125", 2)` → `"0.12"`; `"0.135"` → `"0.14"`; `"0.000000005"` com 6 → `"0.000000"`;
  - `formatMoney("0.108000000","BRL")` → `"R$ 0,11"`;
  - `formatMoney("0.004000000","USD")` → `"US$ 0,004"`;
  - `formatMoney("12345678.900000000","BRL")` → `"R$ 12.345.678,90"`.
  - Os casos de `format.ts` copiados dos scripts da Fase 8.
- [ ] **Step 2: Run** `cd frontend && npm test`. Expected: FAIL (`money.ts` não existe; `format.test.ts` passa).
- [ ] **Step 3: Implement;** `npm install -D vitest` (versão estável atual).
- [ ] **Step 4: Run** `npm test` e `make check`. Expected: PASS.

### Task 8: Telas Custos e Preços

**Files:**
- Create:
  - `frontend/src/features/analytics/CostsPage.tsx`;
  - `frontend/src/features/analytics/PricesPage.tsx`;
  - `frontend/src/features/analytics/PriceForm.tsx`;
  - `frontend/src/features/analytics/RecalculateDialog.tsx`;
  - `frontend/src/features/analytics/CostSessionsTable.tsx`.
- Modify:
  - `frontend/src/App.tsx`: rotas `analytics/costs` e `analytics/costs/prices`;
  - `AnalyticsLayout.tsx`: abas Custos e Padrões; filtro de modelo também em `/analytics/costs`;
  - `useAnalyticsData.ts`: recarregar também em `costs:updated`.

**Interfaces:**
- Consumes: os métodos do `CostService` (Task 4/6); `formatMoney` e `moneyStatusLabel` (Task 7); `AnalyticsCard`, `dataView`, `RefreshError`, `AreaChart`, `StackedBar` e `GroupTable` da Fase 8.

- [ ] **Step 1: CostsPage.**
  - **Topo:** selo "Estimativa" e o aviso fixo.
  - **Total:** cartões de total por moeda, com variação contra o período anterior e a cobertura "estimativa sobre N% das respostas".
  - **Qualidade:** completas, parciais, sem preço e sem dados.
  - **Evolução:** série com uma linha por moeda e seletor Dia / Semana / Mês (`bucket`).
  - **Detalhamento:** por agente, modelo e projeto, mais a composição por categoria (StackedBar por moeda).
  - **Sessões:** `CostSessionsTable` com link para `/session?...`, igual a `SessionsUsageTable`, e lista "Sessões sem custo calculável" com os motivos e o link.
  - **Botões:** Recalcular (abre o diálogo com a prévia: N estimativas mudam, total antes → depois por moeda) e Exportar CSV (avisa o caminho salvo).
  - **Sem preço cadastrado:** estado vazio com o botão "Cadastrar preços".
  - "sem preço" nunca aparece como valor 0.
- [ ] **Step 2: PricesPage e PriceForm.**
  - **Tabela:** agente, modelo, moeda, unidade, as quatro categorias, vigência, origem e o estado vigente/desativado.
  - **Ações:** editar, "novo preço a partir de…", desativar e reativar.
  - **Histórico:** expande as revisões com a data e o número de estimativas que usam cada uma.
  - **Aviso na edição:** "Estimativas já calculadas não mudam. Use Recalcular para aplicar."
  - **Formulário:**
    - unidade em select (por token, por mil, por milhão);
    - moeda preenchida com `DefaultCurrency`;
    - a categoria vazia significa sem preço, com dica;
    - o erro do backend aparece no formulário;
    - no topo da página, select da moeda padrão (`SetDefaultCurrency`).
- [ ] **Step 3: Verificar:** `make check` e `make build`.
- [ ] **Step 4: Verificação visual.** Rodar com `packaging/scripts/demo-home.py` (`HOME` de exemplo). Cadastrar um preço, ver os custos, editar, recalcular, exportar e conferir o arquivo. Capturar telas para o relatório.

### Task 9: Tela Padrões

**Files:**
- Create:
  - `frontend/src/features/analytics/PatternsPage.tsx`;
  - `frontend/src/components/charts/CalendarGrid.tsx`;
  - `frontend/src/components/charts/Histogram.tsx` (barras simples por faixa).
- Modify: `frontend/src/App.tsx` (rota `analytics/patterns`).

**Interfaces:**
- Consumes: `PatternsService.Patterns` e `ExportCSV` (Tasks 5/6); o componente de heatmap da `ActivityPage`.

- [ ] **Step 1: PatternsPage.**
  - **Topo:** o aviso fixo.
  - **Cartões:** sessões iniciadas (com variação) e retomadas; dias e semanas ativos; sequência atual e maior; sessões por dia ativo.
  - **Gráficos:** calendário, série de iniciadas (com a anterior tracejada) e heatmap.
  - **Projetos:** os mais ativos (sessões, mensagens, dias ativos, dias desde a última) e os recorrentes.
  - **Distribuições:** duração (com o rótulo fixo), mensagens por sessão e intervalo entre sessões.
  - **Agentes:** vários agentes no mesmo projeto e comparação entre agentes.
  - **Botão:** Exportar CSV.
- [ ] **Step 2: Verificar** `make check`, `make build` e a verificação visual com o `demo-home`.

### Task 10: Documentação e fechamento

**Files:**
- Modify: `docs/ANALYTICS.md`, `docs/DOMAIN.md`, `docs/ARCHITECTURE.md`, `docs/ROADMAP.md`, `README.md` (seção Analytics, se houver), `CHANGELOG.md`

- [ ] **Step 1: `ANALYTICS.md`.**
  - **Custos:**
    - fórmula, unidades e nanos;
    - status e motivos;
    - vigência e precedência de modelo;
    - editar × novo preço a partir de × fechar × desativar;
    - quando a estimativa muda (Refresh × Recalcular);
    - moedas;
    - CSV (colunas e escape).
  - **Padrões:** semântica de cada indicador (tabela da spec §5 mais as decisões 12 e 13) e o aviso fixo.
  - **Desempenho:** números medidos.
  - **Limitações:** spec §11.
- [ ] **Step 2: `DOMAIN.md`** (PriceRevision, Estimate, status), **`ARCHITECTURE.md`** (user.db novo, ATTACH, sync → Refresh → `costs:updated`, CostService e PatternsService) e **`ROADMAP.md`** (Fase 9 ✔ com data; pendências "para a Fase 9" viram gerais; `Refresh` incremental; Vitest entrou).
- [ ] **Step 3:** `make check`, `make build` e `gofmt -l .` vazio.
- [ ] **Step 4: Revisão final do branch** (fresh reviewer) e relatório da fase:
  - o que foi feito;
  - arquivos;
  - decisões;
  - testes;
  - limitações;
  - pendências.

  Não iniciar a Fase 10.
