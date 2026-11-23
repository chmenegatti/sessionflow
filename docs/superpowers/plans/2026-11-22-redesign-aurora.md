# Redesign Aurora e Dashboard analítico — Plano de implementação

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** aplicar a identidade visual Aurora (vidro, brilhos e translucidez) a todas as telas do SessionFlow e refazer o Dashboard com painéis analíticos alimentados por um serviço novo de estatísticas.

**Architecture:**
- **Backend.** A migration 0003 acrescenta a `messages` os campos `has_text` (mesma regra de `message_count`, via `domain.CountsAsMessage`), `tools` (array JSON com os nomes das ferramentas chamadas) e um índice em `ts`. Um store de estatísticas agrega por intervalo (dia, semana ou mês, no fuso local) e o `service.StatsService` monta tudo o que o Dashboard precisa numa chamada.
- **Frontend.** Os tokens do tema e três superfícies (`.glass` com desfoque só nas peças flutuantes, `.surface` translúcida sem desfoque, `.surface-sel`) dão o visual a todas as telas pelos componentes compartilhados. Os gráficos são componentes SVG próprios.

**Tech Stack:** Go 1.26 · modernc SQLite (FTS5, `json_each`) · Wails v3 · React 19 · Tailwind v4 · motion · `@fontsource-variable/inter` e `@fontsource-variable/jetbrains-mono`.

**Spec:** `docs/superpowers/specs/2026-11-22-redesign-aurora-design.md`. Maquete de referência: `docs/superpowers/specs/2026-11-22-redesign-aurora/tema-b.png`; o HTML dela, `index.html` na mesma pasta, tem os valores de CSS (tema `b`).

## Global Constraints

- **Não fazer commit** (regra do usuário). Onde a skill pede "Commit", o passo é o checkpoint: `make check` verde e diff revisado.
- Textos da interface em pt-BR; nomes de código e de comandos como já estão no projeto.
- Tema escuro único. Paleta exata da spec §1: fundo `#060914`, texto `#eef2ff`/`#9aa3c7`/`#636c93`, acento `#38e1f5`, acento 2 `#a78bfa`, positivo `#5eead4`, negativo `#fb7185`, Claude `#fb923c`, Codex `#38e1f5`, OpenCode `#c084fc`. Raios de 18 px (card), 10 px (controle) e 999 px (pílula).
- `backdrop-filter` só em `.glass`: barra lateral, cabeçalho, painel de detalhes, diálogos, avisos, tooltips e menus. Nunca em cards, linhas de lista ou viewer.
- Sem biblioteca de gráficos e sem fonte vinda da rede. Fontes pelo npm (`@fontsource-variable/inter`, `@fontsource-variable/jetbrains-mono`); a Geist sai do `package.json`.
- Animações respeitam "reduzir movimento" (`MotionConfig reducedMotion="user"` e `useReducedMotion()` em laços JS). Nada anima nem esmaece em recargas por `dataVersion`.
- Sessões ocultas (subagentes e automáticas: `parent_native_id IS NULL AND automated = 0` para as visíveis) ficam fora de todas as estatísticas.
- Nenhum conteúdo de conversa no log. As consultas do Dashboard ficam abaixo de 150 ms no índice local (cerca de 58 mil mensagens).
- `make check`, `make build` e `make test-install` continuam passando.

## Review Focus

1. **Índice vazio ou período sem atividade** (instalação nova): o Dashboard mostra zeros, gráficos planos sem `NaN`, frase-resumo curta ("Nenhuma atividade nos últimos 30 dias") e tabela com estado vazio. Teste no Task 5 (`TestDashboardEmpty`).
2. **Mensagens sem `ts`**: ficam fora das séries sem quebrar a consulta. Teste no Task 3 (`TestActivityIgnoresMissingTimestamp`).
3. **Virada do dia no fuso local**: uma mensagem às 23:30 em -03:00 conta no dia local, não no dia UTC. Teste no Task 3 (`TestActivityUsesLocalDay`).
4. **Logo depois da migration 0003**, antes da releitura: `has_text`/`tools` zerados. A migration limpa `sync_state`, a indexação completa refaz tudo e o Dashboard atualiza pelo `index:updated`. Teste no Task 2 (`TestMigration0003ForcesReindex`).
5. **Todos os agentes desligados nos filtros**: a interface não deixa desligar o último; no backend, lista vazia = todos. Teste no Task 5 (`TestDashboardEmptyProvidersMeansAll`) e regra no Task 9.

---

### Task 1: Regra única de "mensagem" no domínio

**Files:**
- Modify: `internal/domain/message.go` (função nova)
- Modify: `internal/provider/claude/claude.go:548-572`, `internal/provider/codex/codex.go:405-420`, `internal/provider/opencode/opencode.go:283-286` (passam a usar a função)
- Test: `internal/domain/message_test.go`

**Interfaces:**
- Produces: `func CountsAsMessage(m Message) bool`: verdadeiro se `Role` é `RoleUser` ou `RoleAssistant` **e** há ao menos um bloco `BlockText` com texto não vazio depois de `strings.TrimSpace`.

- [ ] **Step 1: Teste** `TestCountsAsMessage` (tabela):
  - usuário com texto → true;
  - usuário só com imagem → false;
  - agente com texto e chamada de ferramenta → true;
  - agente só com ferramenta → false;
  - `RoleTool` com resultado → false;
  - `RoleSystem` com texto → false;
  - texto só com espaços → false.
- [ ] **Step 2:** `go test ./internal/domain/ -run CountsAsMessage` falha (função não existe).
- [ ] **Step 3:** implementar e trocar `hasText`/`firstText` dos três adaptadores por `domain.CountsAsMessage(m)` no cálculo de `MessageCount`. Remover os `hasText` que ficarem sem uso.
- [ ] **Step 4:** `go test ./internal/...` passa. Algum teste de provider pode ter `MessageCount` esperado diferente (usuário só com imagem deixa de contar no Claude e no Codex); ajustar o valor esperado e citar o caso no comentário do teste.
- [ ] **Step 5: Checkpoint.**

### Task 2: Migration 0003 e preenchimento pelo indexador

**Files:**
- Create: `internal/repository/sqlite/migrations/index/0003_stats.sql`
- Modify: `internal/repository/sqlite/index_store.go:192-198` (`IndexedMessage`), o `INSERT` de `BeginSource`, `AddMessage`
- Modify: `internal/indexer/indexer.go:343-351` (sink)
- Test: `internal/repository/sqlite/db_test.go`, `internal/indexer/indexer_test.go`

**Interfaces:**
- Produces: colunas `messages.has_text INTEGER NOT NULL DEFAULT 0` e `messages.tools TEXT NOT NULL DEFAULT '[]'` (array JSON de nomes, com repetição: duas chamadas a Bash = `["Bash","Bash"]`); índice `messages_ts_idx ON messages(ts)`; `DELETE FROM sync_state`.
- Produces: `IndexedMessage` ganha `HasText bool` e `Tools []string`.
- Decisão do plano: `tools` vira array JSON, e não texto separado por espaço como dizia a spec, para a contagem sair em SQL com `json_each`. O efeito é o mesmo.

- [ ] **Step 1: Testes.**
  - `TestMigration0003ForcesReindex`: abrir índice na versão 2 com uma linha em `sync_state`; depois de `OpenIndex`, a versão é 3, `sync_state` está vazio e as colunas existem.
  - `TestIndexerFillsHasTextAndTools`: com o fixture Claude (`docs/discovery/fixtures/claude/session.jsonl`), depois de `Run` existe linha com `has_text=1`, existe linha de agente só com ferramenta com `has_text=0` e `tools='["Bash"]'` (ou a ferramenta do fixture), e a soma de `has_text` da sessão é igual a `sessions.message_count`.
- [ ] **Step 2:** os dois falham.
- [ ] **Step 3:** escrever a migration, os campos e o sink: `HasText: domain.CountsAsMessage(m)`, `Tools` com os `ToolName` dos blocos `BlockToolCall`, em ordem. O `INSERT` grava `json.Marshal(tools)`, com `[]` quando vazio.
- [ ] **Step 4:** os testes passam; `go test -race ./internal/...` verde.
- [ ] **Step 5: Checkpoint.**

### Task 3: Store de estatísticas (SQL)

**Files:**
- Create: `internal/repository/sqlite/stats_store.go`
- Test: `internal/repository/sqlite/stats_store_test.go`

**Interfaces:**
- Consumes: colunas do Task 2; `visibleS` e `sessionCols` de `session_store.go`.
- Produces:
```go
type Bucket string // "day" | "week" | "month"
type StatsFilter struct {
	From, To  time.Time           // [From, To); From zero = desde o início
	Providers []domain.ProviderID // vazio = todos
	Bucket    Bucket
	Offset    int                 // segundos do fuso local (time.Now().Zone())
}
type ActivityRow struct { Bucket string; Provider domain.ProviderID; Sessions, Messages int } // Bucket = "AAAA-MM-DD" do início local do intervalo
type Totals struct { Sessions, Messages, Projects int }
type NamedCount struct { Name, Path string; Count int }              // Path só para projetos
func (s *IndexStore) Activity(ctx, f StatsFilter) ([]ActivityRow, error)
func (s *IndexStore) ProjectsPerBucket(ctx, f StatsFilter) (map[string]int, error)
func (s *IndexStore) StatsTotals(ctx, f StatsFilter) (Totals, map[domain.ProviderID]int, error) // sessões por provider
func (s *IndexStore) TopProjects(ctx, f StatsFilter, n int) ([]NamedCount, error)              // por mensagens
func (s *IndexStore) TopTools(ctx, f StatsFilter, n int) ([]NamedCount, error)                 // json_each(messages.tools)
func (s *IndexStore) FirstActivity(ctx, providers []domain.ProviderID) (time.Time, error)       // menor ts visível; zero se vazio
func (s *IndexStore) TotalProjects(ctx, providers []domain.ProviderID) (int, error)
```
- Regras:
  - "Sessão ativa no intervalo" = tem mensagem `has_text=1` com `ts` no intervalo (contagem distinta por sessão).
  - "Mensagens" = `count(*)` com `has_text=1`.
  - Dia local = `(ts/1000 + Offset) / 86400`.
  - Semana começa na segunda.
  - Mês = `strftime('%Y-%m-01', dia*86400, 'unixepoch')`.
  - Linhas sem `ts` ficam de fora.
  - Sempre com `visibleS` e o filtro de providers.

- [ ] **Step 1: Testes** com sessões gravadas pelo `BeginSource`/`AddMessage`, como em `search_test.go`:
  - `TestActivityByDay`: 2 sessões Claude e 1 Codex em 3 dias. Linhas por dia e por provider com `Sessions` distintas e `Messages` só de `has_text`.
  - `TestActivityUsesLocalDay`: `ts` = 2026-11-10T02:30Z com `Offset=-10800` cai em "2026-11-09".
  - `TestActivityIgnoresMissingTimestamp`: mensagem sem `ts` não aparece e não dá erro.
  - `TestActivityWeekAndMonth`: dias 2026-11-10 (seg) e 2026-11-16 (dom) caem na semana "2026-11-10"; mês "2026-11-01".
  - `TestStatsExcludesHidden`: subagente e automática não contam.
  - `TestTopToolsAndProjects`: Bash×3, Edit×1 → `[{Bash 3} {Edit 1}]`; projetos ordenados por mensagens, com `Name` = nome curto e `Path`.
  - `TestStatsTotalsProviderFilter`: só Codex → totais só do Codex.
- [ ] **Step 2:** falham.
- [ ] **Step 3:** implementar. A expressão de intervalo vem de uma função Go conforme `Bucket` (constante, nunca texto do usuário).
- [ ] **Step 4:** passam.
- [ ] **Step 5: Checkpoint.**

### Task 4: Favoritas por data no `user.db`

**Files:**
- Modify: `internal/repository/sqlite/user_store.go`
- Test: `internal/repository/sqlite/user_store_test.go` (criar se não existir)

**Interfaces:**
- Produces: `func (s *UserStore) FavoritesSince(ctx, since time.Time) (map[string]time.Time, error)`: chave `"provider:id"` → `created_at`, para as favoritas com `created_at >= since` (zero = todas).

- [ ] **Step 1: Teste** `TestFavoritesSince`: 3 favoritas em datas diferentes; `since` no meio devolve 2; zero devolve 3.
- [ ] **Step 2:** falha. **Step 3:** implementar. **Step 4:** passa. **Step 5: Checkpoint.**

### Task 5: `StatsService` (Dashboard, projetos fixados, retomar última)

**Files:**
- Create: `internal/service/stats.go`
- Modify: `app.go` (registrar o serviço); `make bindings`
- Test: `internal/service/stats_test.go`

**Interfaces:**
- Consumes: Tasks 3 e 4; `SessionService.items` (converte linhas em `SessionItem` com favorito e tags; extrair para uma função do pacote que os dois serviços usem); a regra de sessão ativa do `ResumeService.active` (extrair para `sessionActive(s domain.Session, now time.Time) bool` no pacote `service`, usada pelos dois).
- Produces:
```go
type StatsQuery struct { Period string `json:"period"`; Providers []domain.ProviderID `json:"providers"` } // "7d"|"30d"|"90d"|"all"; inválido → ErrInvalidFilter
type Metric struct { Value int `json:"value"`; Prev int `json:"prev"`; HasPrev bool `json:"hasPrev"` }
type SeriesPoint struct { Start int64 `json:"start"`; ByProvider map[domain.ProviderID]int `json:"byProvider"`; Total int `json:"total"` }
type ProviderShare struct { Provider domain.ProviderID `json:"provider"`; Sessions int `json:"sessions"` }
type Dashboard struct {
	Period string `json:"period"`; From int64 `json:"from"`; To int64 `json:"to"`; Bucket string `json:"bucket"`
	Sessions, Messages, ActiveProjects Metric; TotalProjects int `json:"totalProjects"`
	Favorites int `json:"favorites"`; FavoritesAdded int `json:"favoritesAdded"`; OpenNow int `json:"openNow"`
	SessionsSeries, MessagesSeries []SeriesPoint      // um ponto por intervalo, intervalos vazios com zero
	PrevSessions, PrevMessages []int                  // mesmo tamanho; vazio em "all"
	ProjectsSeries, FavoritesSeries []int             // mini-gráficos
	ByProvider []ProviderShare; TopProjects, TopTools []sqlite.NamedCount
	Peak int64 `json:"peak"`                          // início do intervalo de maior total de mensagens; 0 se não há
	Recent RecentTabs `json:"recent"`                 // All, Open, Favorites []SessionItem (até 5 cada)
	ResumeID string `json:"resumeId"`                // sessão principal mais recente não aberta, dentro do filtro; "" = nenhuma
}
func (s *StatsService) Dashboard(ctx context.Context, q StatsQuery) (Dashboard, error)
func (s *StatsService) PinnedProjects(ctx context.Context) ([]sqlite.NamedCount, error) // top 3 por mensagens nos últimos 30 dias, todos os providers
```
- Regras:
  - **Período:** `To` = agora. `From` = início do dia local de hoje menos (N−1) dias. Anterior = `[From − (To − From), From)`.
  - **"all":** `From` = `FirstActivity`; intervalo = semana se o histórico tem até 2 anos, senão mês; sem período anterior (`HasPrev=false`, `Prev*` vazios).
  - **Outros períodos:** intervalo diário.
  - **"Abertas":** sessões visíveis com `UpdatedAt` nos últimos 10 min, filtradas por `sessionActive`.
  - Favoritas, abertas e totais respeitam o filtro de agente (spec, "Filtros").
  - **`Recent.Favorites`:** favoritas por atividade recente.
  - Os filtros valem para as três abas.

- [ ] **Step 1: Testes.**
  - `TestDashboardEmpty`: índice vazio devolve zeros, 30 pontos com `Total=0` e `ResumeID=""`, sem erro.
  - `TestDashboardPeriods`: com relógio injetado (`now func() time.Time`), "7d" tem 7 pontos e `HasPrev=true`; o período anterior conta só mensagens de 7 a 14 dias atrás.
  - `TestDashboardAllUsesWeeks`: atividade de 400 dias gera intervalos semanais; mais de 2 anos gera mensais; `HasPrev=false`.
  - `TestDashboardEmptyProvidersMeansAll`.
  - `TestDashboardProviderFilterAppliesToTotals`: filtro Codex muda `Favorites` e `TotalProjects`.
  - `TestDashboardResumeSkipsOpen`: a mais recente tem o arquivo alterado agora, então `ResumeID` é a segunda.
  - `TestDashboardPeak`.
  - `TestPinnedProjects`: no máximo 3, ordenados.
  - `TestDashboardQueryTime`: com 50 mil mensagens geradas, `Dashboard` leva menos de 150 ms; pular com `-short`.
- [ ] **Step 2:** falham.
- [ ] **Step 3:** implementar, registrar no `app.go` e rodar `make bindings`.
- [ ] **Step 4:** `go test -race ./internal/...` verde.
- [ ] **Step 5: Checkpoint.**

### Task 6: Sistema visual (tokens, fontes, superfícies, componentes base)

**Files:**
- Modify: `frontend/package.json` (adicionar as duas `@fontsource-variable`, remover `@fontsource-variable/geist`), `frontend/src/main.tsx` (imports das fontes), `frontend/src/index.css`
- Modify: `frontend/src/components/ui/{card,button,badge,input,native-select,tooltip,skeleton}.tsx`, `components/layout/EmptyState.tsx`, `components/ui/{border-beam,text-shimmer,spotlight}.tsx` (cores fixas → tokens)
- Create: `frontend/src/components/ui/segmented.tsx`

**Interfaces:**
- Produces:
  - classes `.glass`, `.surface`, `.surface-sel` e `.aurora-bg` em `index.css` (valores da spec §1 e do tema `b` da maquete);
  - tokens shadcn remapeados no bloco `.dark`;
  - fontes: `--font-sans: 'Inter Variable'`, `--font-mono: 'JetBrains Mono Variable'`;
  - `Segmented<T extends string>({ value: T; options: {value: T; label: ReactNode}[]; onChange(v: T): void; size?: "sm"|"md" })`: grupo de botões com `role="radiogroup"` e item ativo em acento a 16% com contorno;
  - `Button variant="default"`: gradiente ciano→violeta, brilho e realce interno; as variantes `outline`/`ghost`/`secondary` ficam translúcidas;
  - `Card` usa `.surface`.

- [ ] **Step 1:** `npm i` e `npm run typecheck` verde depois da troca de fontes.
- [ ] **Step 2:** implementar tokens, superfícies e componentes.
- [ ] **Step 3: Verificação visual:** `make server`, abrir `http://localhost:18745/#/` no Chrome e capturar a tela. Botões, cards, pílulas, campos e tooltips seguem a maquete (cores e raios), e o texto tem contraste legível sobre o fundo.
- [ ] **Step 4:** `make check` verde. **Checkpoint.**

### Task 7: Shell (fundo Aurora, barra lateral, cabeçalho, painel, diálogos)

**Files:**
- Modify: `frontend/src/components/layout/{AppLayout,Sidebar,PageHeader,DetailPanel,AppLogo}.tsx`, `features/resume/ResumeOverlays.tsx`, `features/startup/StartupGate.tsx`

**Interfaces:**
- Consumes: `StatsService.PinnedProjects` (Task 5), recarregado por `dataVersion`; `.glass`/`.aurora-bg` (Task 6).
- Produces: `PageHeader({ title; actions?; crumbs?: ReactNode })`. Quando `crumbs` é passado, ele substitui o título.

- [ ] **Step 1:**
  - **AppLayout:** camada `.aurora-bg` fixa atrás de tudo.
  - **Sidebar:** em `.glass`; item ativo com superfície 2, sombra e marcador em gradiente com brilho; seção "Projetos fixados" (ao clicar, abre `projectPath(p.path)`); rodapé em card com "Sincronizado · há N s" a partir de `status.scan.finishedAt`, contagem de fontes e barra em gradiente; o estado "Indexando…" continua.
  - **PageHeader:** em `.glass`, fixo.
  - **DetailPanel, ResumeConfirm, NoticeToast e tela de erro:** em `.glass`.
- [ ] **Step 2: Verificação visual** com capturas do Dashboard e de Sessões com o painel de detalhes aberto. A rolagem da lista longa ("Mostrar subagentes" ligado, mais de 500 linhas) continua fluida.
- [ ] **Step 3:** `make check`. **Checkpoint.**

### Task 8: Componentes de gráfico (SVG)

**Files:**
- Create: `frontend/src/components/charts/{scale.ts,Sparkline.tsx,AreaChart.tsx,Donut.tsx,BarList.tsx}`

**Interfaces:**
- Produces:
  - `Sparkline({ values: number[]; color: string; height?: number })`.
  - `AreaChart({ points: {start: number; byKey: Record<string, number>}[]; keys: {key: string; color: string; label: string}[]; previous?: number[]; formatTick(start: number): string; formatTooltip(start: number): string; onSelect?(start: number): void })`:
    - áreas empilhadas na ordem de `keys`;
    - linha tracejada para `previous`;
    - grade com 4 linhas;
    - linha vertical e balão de vidro no hover;
    - `drop-shadow` na cor de cada série.
  - `Donut({ parts: {value: number; color: string}[]; center: ReactNode; size?: number })`.
  - `BarList({ items: {label: string; value: number; onClick?(): void}[]; color: "accent"|"accent2" })`.
  - **`scale.ts`:** `linear(domainMax: number, rangePx: number): (v: number) => number` com `domainMax = 0` → função que devolve a base (nunca `NaN`); `areaPath(...)`.
- Gráficos com todos os valores 0 desenham linha na base, sem `NaN`.

- [ ] **Step 1:** implementar seguindo o `<script>` da maquete (`index.html`), que já tem os gradientes, o brilho e o balão no visual aprovado.
- [ ] **Step 2:** `npm run typecheck`. A verificação visual fica com o Task 9.
- [ ] **Step 3: Checkpoint.**

### Task 9: Dashboard

**Files:**
- Modify: `frontend/src/features/dashboard/DashboardPage.tsx` (reescrita)
- Create: `frontend/src/features/dashboard/{summary.ts,KpiCard.tsx,ActivityCard.tsx,AgentsCard.tsx,RecentTable.tsx}`
- Modify: `frontend/src/lib/api.ts` (exports do `StatsService` e tipos)

**Interfaces:**
- Consumes: `StatsService.Dashboard` (Task 5); componentes dos Tasks 6 e 8; `useResume()` e `ResumeService` já existentes; `providersOf(status)` para o estado de detecção na legenda dos agentes.
- Produces: `summary(d: Dashboard, providerNames: Record<string,string>): {text: string; bold: boolean}[]`: segmentos da frase-resumo.
  - Partes condicionais da frase: abertas só se `openNow > 0`; concentração só com 2 ou mais agentes com sessões; pico só se `peak > 0`; sem atividade, "Nenhuma atividade nos últimos N dias."

- [ ] **Step 1: Montar a página** na ordem da spec §4:
  - cabeçalho com data e "N agentes detectados", título "Suas sessões" com gradiente, frase-resumo;
  - filtros: `Segmented` de período (padrão "30d"), pílulas de agente com contagem (o último ligado não desliga) e "Atualiza sozinho";
  - 4 `KpiCard`: Sessões e Mensagens clicáveis trocam a métrica do gráfico (padrão Mensagens); variação omitida quando `!hasPrev`;
  - `ActivityCard` com `AreaChart`: clicar num intervalo navega para `/sessions` (sem filtro de data; ver Task 10);
  - linha de 3 cards: `AgentsCard` (rosca, legenda com estado de detecção, ocultas, `providerNote` no tooltip, `BorderBeam` quando `scan.running`), projetos mais ativos (clicar abre o projeto) e ferramentas mais usadas;
  - `RecentTable` com abas Todas, Abertas e Favoritas;
  - no cabeçalho, campo "Buscar em todas as conversas · Ctrl K" (navega para `/search`) e "Retomar última" (`resume(d.resumeId)`; desabilitado com dica quando vazio).
  - Recarrega por `dataVersion` sem esmaecer (a resposta leva a chave do pedido, como em `SessionList`).
- [ ] **Step 2: Verificação visual** com dados reais (`make server`): a captura do Dashboard fica próxima de `tema-b.png`. Também com índice vazio (`XDG_DATA_HOME` temporário): zeros, sem `NaN` e frase curta.
- [ ] **Step 3:** `make check`. **Checkpoint.**

### Task 10: Demais telas

**Files:**
- Modify: `frontend/src/features/sessions/{SessionList,SessionRow,SessionRows,ProviderTag,FavoriteButton,TagEditor}.tsx`, `features/projects/{ProjectsPage,ProjectSessionsPage}.tsx`, `features/search/SearchPage.tsx`, `features/viewer/{ViewerPage,MessageRow,Blocks,Markdown}.tsx`, `features/settings/{SettingsPage,ProviderBadge}.tsx`

- [ ] **Step 1: Listas e projetos.**
  - Barra de filtros com `Segmented` (ordenação) e pílulas de agente.
  - Lista dentro de card `.surface` com o realce que desliza em `bg-white/[0.04]` e borda ciano sutil na selecionada.
  - `ProviderTag` com cores da spec (15% de fundo, contorno a 30%).
  - Projetos em card com pílulas, sessões e última atividade.
- [ ] **Step 2: Busca e Configurações.**
  - Busca: campo grande em `.surface` com brilho ciano no foco; resultados em card; `<mark>` com fundo ciano a 20%.
  - Configurações: cards `.surface` e campos translúcidos.
- [ ] **Step 3: Viewer.**
  - Mensagem do usuário em bolha `.surface`.
  - Recolhíveis em superfície 2 com borda.
  - Tema de destaque de código com as cores da paleta (escolha do implementador: substitui a folha atual do highlight.js).
  - Mensagem-alvo da busca com borda ciano e brilho.
  - Sem `backdrop-filter`.
- [ ] **Step 4: Verificação.**
  - Capturas de Sessões, Projetos, Busca com resultado, Viewer com bloco de ferramenta aberto, Configurações e painel de detalhes.
  - Rolagem do viewer na maior sessão local (mais de 30 mil registros) sem travar.
  - O clique no dia do gráfico (Task 9) só abre `/sessions`: registrar em `docs/ROADMAP.md` que o filtro por data na listagem ficou pendente.
- [ ] **Step 5:** `make check`. **Checkpoint.**

### Task 11: Fechamento

**Files:**
- Modify: `docs/ARCHITECTURE.md` (seção Frontend: superfícies e gráficos; seção nova "Estatísticas"), `docs/ROADMAP.md`, `CHANGELOG.md` (seção "Não lançado": redesign Aurora, Dashboard analítico, migration 0003 com releitura do índice), `README.md` (trocar `docs/img/dashboard.png` pela captura nova feita com dados de fixture)

- [ ] **Step 1:** `make check`, `make build` e `make vuln` verdes.
- [ ] **Step 2:** `make release` e `make test-install`. Os 6 alvos suportados passam, e as capturas em `dist/test/*/screenshot.png` mostram o tema Aurora. O critério de "interface renderizada" do teste (pixel da barra lateral abaixo de 60) continua válido com o fundo novo; ajustar se o valor mudar.
- [ ] **Step 3:** atualizar a documentação e a captura do README. **Checkpoint final**, sem commit, com o relatório ao usuário.
