# Arquitetura

Monólito modular em Go, com frontend React embutido via Wails v3. Base: [docs/discovery/00-relatorio.md §6](discovery/00-relatorio.md).

## Visão geral

```
            ┌──────────────── frontend (React, WebKitGTK) ────────────────┐
            │ features/* → lib/store (Zustand) → lib/api → bindings (gerados) │
            └───────────────────────────────┬──────────────────────────────┘
                                            │ chamadas Wails (JSON)
┌──────────────────────────────── processo Go ───────────────────────────────┐
│ main.go / app.go   wiring explícito, sem estado global                      │
│   config ──► logging ──► repository/sqlite (index.db, user.db) ──► service  │
│                                                         ▲                   │
│   provider/{claude,codex,opencode} ──► indexer ─────────┘                   │
│   lockfile (instância única) · watcher (Fase 6) · terminal (Fase 6)         │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Módulos

| Pacote | Responsabilidade | Depende de | Estado |
|---|---|---|---|
| `internal/domain` | Tipos do modelo (`SessionKey`, `Session`, `Message`, `Block`, `Project`, `ProviderID`, `Usage`, `PriceRevision`, `Estimate`) e invariantes; cálculo de custo puro (`cost.go`) | nada | Fases 1, 8, 9 |
| `internal/config` | Diretórios XDG; `config.json` (carregar, validar, salvar de forma atômica); raiz padrão de cada provider | domain | Fase 1 |
| `internal/logging` | slog: JSON em arquivo + texto no stderr; rotação simples; redação de chaves com conteúdo; piso de nível para o logger do Wails | — | Fase 1 |
| `internal/repository/sqlite` | Abertura com PRAGMAs, migrations embutidas (`user_version`), FTS5, recuperação do índice | modernc.org/sqlite | Fase 1 |
| `internal/provider` | Contrato `Provider` (`Detect`, `Sources`, `Parse` → `Sink`) e utilitários (CLI, tempo, truncamento) | domain | Fase 2 |
| `internal/provider/jsonl` | Leitor JSONL em streaming: só linhas completas, limite de 32 MB por linha | — | Fase 2 |
| `internal/provider/sqliteread` | Abre SQLite de terceiros sem criar arquivos no diretório deles (`mode=ro` com `-shm`, senão cópia privada) | modernc.org/sqlite | Fase 2 |
| `internal/provider/{claude,codex,opencode}` | Adaptadores: descoberta, detecção, parse e normalização | provider, domain | Fase 2 |
| `internal/indexer` | Percorre os providers, pula fontes inalteradas, grava sessões e texto pesquisável; isola falhas por provider e por fonte | provider, sqlite | Fase 2 |
| `internal/lockfile` | Instância única (`flock` em `$XDG_DATA_HOME/sessionflow/sessionflow.lock`) | — | Fase 2 |
| `internal/service` | Serviços Wails: `AppService.Status`; `SyncService` (indexação em segundo plano, evento `index:updated`); `SessionService` (listagem, detalhes, projetos, favoritos, tags); `TranscriptService` (viewer, `Open`/`OpenAt`); `SearchService` (busca global); `ResumeService` (retomada); `SettingsService` + `ConfigStore` (config.json pela interface); `NewProviders` | config, indexer, provider, sqlite, terminal | Fases 1–6 |
| `internal/watcher` | fsnotify nas pastas dos providers; espera por silêncio (1 s, no máximo 5 s) e avisa quais providers mudaram | fsnotify, provider | Fase 6 |
| `internal/terminal` | Emuladores conhecidos (sintaxe de cada um), detecção, modelo personalizado com `{dir}`/`{cmd}`, exec direto sem shell, processo desligado do app | — | Fase 6 |
| `internal/host` | AppImage: ambiente limpo para processos filhos (sem `LD_LIBRARY_PATH`, `GTK_*` etc. do AppRun) e verificação de `bubblewrap`/`xdg-dbus-proxy`, exigidos pelo WebKit | — | Fase 7 |
| `frontend/src` | UI: layout, rotas, estado de UI; **sem regra de negócio** | bindings | Fase 1 |

## Inicialização (`app.go`)

`main.go` trata `--version`, `--check` (diagnóstico sem janela, só leitura), `--test-terminal DIR CMD…` (abre o terminal como a retomada) e `--help` antes de qualquer outra coisa.

1. Resolve os diretórios XDG e carrega `config.json`. Se o arquivo não existe, cria o padrão. Se é inválido, usa o padrão e registra um aviso, **sem sobrescrever** o arquivo.
2. Cria o logger (nível definido na config). Se o arquivo de log não abre, segue só com stderr.
3. Abre `index.db`. Se o SQLite reporta corrupção (`SQLITE_CORRUPT`/`SQLITE_NOTADB`, `quick_check` ou `integrity-check` do FTS) ou o esquema é futuro, o arquivo é movido para `index.db.corrupt-<ts>` e recriado. Outros erros (permissão, disco cheio, banco ocupado) abortam sem mover nada.
4. Abre `user.db`. Nunca é descartado automaticamente; em caso de erro, o app não sobe.
   - Falha fatal (índice, `user.db`, trava de instância): o erro vai para o stderr e uma janela mínima mostra a mensagem e o caminho do log (`StartupService` + `StartupGate` no frontend). Aberto pelo menu do desktop, o app não fecha mais em silêncio.
   - Segunda instância: o lockfile já está preso. O processo chama `application.New` com `SingleInstance`, que avisa a primeira instância por D-Bus (`com.cesarmenegatti.sessionflow.SingleInstance`); ela mostra e foca a janela, e a segunda sai com código 0.
5. Monta os serviços com dependências explícitas (`AppDeps`) e registra-os no Wails.
6. Abre a janela. `app.Run()` bloqueia até o fim e os `defer` fecham os bancos e o log.

## Bancos

| Banco | Conteúdo | Política |
|---|---|---|
| `index.db` | `projects`, `sessions`, `usage` (consumo de tokens, Fase 8), `messages` (com `has_text` e `tools` para as estatísticas), `messages_fts` (FTS5 external content + triggers), `sync_state` | descartável; reconstruído a partir dos históricos |
| `user.db` | `favorites`, `tags`, `session_tags`, `price_revisions`, `cost_estimates` (Fase 9), com chave `(provider, native_id)` | preservado; nunca apagado automaticamente |

PRAGMAs: `journal_mode=WAL`, `foreign_keys=ON`, `busy_timeout=5000`, `synchronous=NORMAL`; transações `BEGIN IMMEDIATE` (`_txlock=immediate`). Arquivos com permissão `0600`.

## Indexação (Fase 2)

1. `SyncService.ServiceStartup` dispara `indexer.Run` numa goroutine. O shutdown cancela e espera.
2. Providers desabilitados na config têm as sessões removidas do índice.
3. Para cada provider habilitado, em sequência: `Detect` → `Sources` → remove as sessões de fontes que sumiram (só se a listagem foi completa; `ErrIncomplete` = algum diretório ilegível, nada é removido) → para cada fonte, compara tamanho, mtime e `Fingerprint` com `sync_state`; fonte inalterada é pulada, exceto se o último estado foi `error` (pode ser transitório) ou `duplicate` (a outra fonte pode ter sumido).
4. Fonte alterada: uma transação apaga as sessões antigas dela (cascade em `messages` e no FTS), o adaptador faz o parse em streaming e o `Sink` grava sessões, projeto e o texto pesquisável (`indexer.SearchText`). Erro ou panic no parse = rollback; a leitura anterior fica e `sync_state.status = error|unsupported`.
5. Provider não encontrado tem as sessões removidas: o índice espelha os históricos.
6. Ao fim, o evento `index:updated` avisa o frontend, que recarrega o `Status`.

Isolamento de falhas: panic em `Detect`/`Sources` vira erro do provider; panic em `Parse` vira erro da fonte. Os demais seguem. O valor do panic não é registrado (pode conter dados lidos), só o tipo e o stack.

Contagens no `Status`: sessões **visíveis** (sem pai e não automáticas) e **ocultas** (subagentes, subsessões, sessões de SDK/hooks), por provider e no total; projetos contam só se têm sessão visível.

Medição nesta máquina (2 GB de históricos Claude, 545 transcripts): primeira indexação 22 s, heap de pico 136 MB, `index.db` de 91 MB; segunda execução 39 ms.

Migrations: `internal/repository/sqlite/migrations/{index,user}/NNNN_nome.sql`, aplicadas em ordem. Cada uma roda numa transação e atualiza `PRAGMA user_version`.

## Sessões, favoritos e tags (Fase 3)

- `IndexStore.ListSessions` filtra (providers, projeto, chaves, ocultas) e ordena por `updated_at` no SQL, com `LIMIT/OFFSET` e total. Sem data vai para o fim.
- Favoritos e tags ficam no `user.db` (`UserStore`). Os bancos não são unidos por `ATTACH`: o `SessionService` busca as chaves no `user.db` (favoritas, tag) e as passa ao índice via `json_each`; depois junta favorito e tags de cada linha da página.
- Favoritar e adicionar tag exigem que a sessão exista no índice; remover tag não (a sessão pode ter sido apagada pelo provider). Tags sem uso são apagadas. Tags são únicas sem diferenciar maiúsculas; o backend normaliza espaços e rejeita vazias, longas (> 40) e com caracteres de controle.
- Visibilidade: a listagem padrão esconde subagentes e sessões automáticas (opção "mostrar"); favoritas e filtro por tag incluem ocultas, porque foram escolhidas pelo usuário.
- `DisplayTitle` remove marcações injetadas pelos providers — só nomes com `-` ou `_` (`<command-name>`, `<pasted_content>`), para não apagar texto como `List<String>` — e cai no primeiro prompt; vazio = "Sem título" na interface.
- A contagem de tags (`Tags`) só considera sessões presentes no índice; o vínculo de uma sessão apagada pelo provider permanece no `user.db` e volta a valer se ela reaparecer.
- Erros técnicos são registrados no log e o frontend recebe mensagem genérica; erros de validação (tag, chave, sessão ausente) vão com texto para o usuário.

## Viewer (Fase 4)

- `TranscriptService.Open(id)` localiza a sessão no índice (fonte e provider) e cria um **cursor**: uma goroutine roda o `Parse` do adaptador (OpenCode: `ParseSession`, só a sessão pedida) com um `Sink` paginador que entrega páginas por um canal sem buffer. O parse fica parado enquanto ninguém pede a próxima página: em memória há no máximo uma página por conversa aberta, mesmo para transcripts de centenas de MB. `Next(cursor)` lê a próxima; `Close` cancela.
- Página: até 100 mensagens ou ~512 KB. Blocos de texto acima de 64 KB e originais (JSON) acima de 8 KB são cortados na visualização (`truncated` = bytes omitidos); o original continua no histórico.
- Até 8 conversas abertas; cursor ocioso por 10 min é descartado; `Open` que falha libera o cursor que criou; `ServiceShutdown` cancela todos e espera no máximo 3 s. Cursor despejado devolve "leitura expirou" (nunca "fim da conversa").
- A fonte é lida com `Source.Raw = true`: metadados levam o JSON original. Na indexação o original não é copiado.
- Frontend: lista virtualizada (`@tanstack/react-virtual`, só as linhas visíveis no DOM, chave estável = `seq`), próxima página pedida perto do fim. Só as 12 páginas mais recentes guardam conteúdo; nas mais antigas cada mensagem vira um esqueleto (seq, papel, horário). A lista não muda de tamanho nem de chaves, então a rolagem não pula (remover páginas e compensar a rolagem foi tentado e medido: o conteúdo saltava). Para rever conteúdo liberado: "Reabrir do início". Conteúdo recolhido (tool calls, resultados, raciocínio, originais) só é montado quando aberto.
- Medição com a maior sessão local (34.970 registros): leitura completa em ~40 s rolando até o fim, ~570 nós no DOM, heap do frontend entre 77 e 116 MB.
- Segurança: Markdown via `react-markdown` sem HTML cru (`skipHtml`); links só `http(s)` e abertos no navegador do sistema (`Browser.OpenURL`), nunca na janela do app; imagens remotas não são carregadas e imagens do histórico não são exibidas (só a referência).

## Busca global (Fase 5)

- Índice: `messages.search_text` + `messages_fts` (FTS5 external content, `unicode61 remove_diacritics 2`), mantido por triggers. A indexação é incremental por fonte (seção acima): fonte alterada = sessões dela apagadas e regravadas na mesma transação; fonte apagada = `RemoveSourcesExcept`; em ambos o cascade em `messages` dispara os triggers e o FTS acompanha. Testado com `integrity-check` do FTS (com conferência do conteúdo) após reescrita, acréscimo e exclusão.
- `service.BuildMatch` traduz o texto digitado: palavras → `"palavra"*` (todas obrigatórias; prefixo só quando o último token tem 3+ letras, pois `a*` expande para boa parte do vocabulário), trechos entre aspas → frase exata. Tudo vai entre aspas, então operadores (`AND`, `OR`, `NOT`, `NEAR`), colunas, parênteses e curingas digitados são texto e a consulta nunca tem erro de sintaxe. Limites: 200 caracteres, 16 termos.
- `IndexStore.SearchMessages`: um JOIN do FTS com `messages`, `sessions` e `projects`; filtros (provider, projeto, ocultas) fora do FTS; ordem `bm25` → mais recente → id (estável entre páginas); total contado até 1001 (`1000+` na interface).
- Trecho: `snippet()` com marcadores U+E000/U+E001; o serviço devolve segmentos `{text, match}`, então o frontend nunca interpreta HTML do histórico. A consulta digitada não vai para o log (pode conter dados).
- Navegação: o resultado leva `seq`; `TranscriptService.OpenAt(id, seq-4)` descarta as mensagens anteriores no backend, o viewer rola até a mensagem, destaca-a e abre os blocos recolhidos dela (o termo costuma estar num resultado de ferramenta). "Ver desde o início" reabre do começo.
- Medição no índice local (58 mil mensagens): 1–90 ms na 1ª página, até ~150 ms em páginas profundas de termos muito comuns.

## Retomada e sincronização (Fase 6)

- **Contrato**: adaptadores implementam `provider.Resumer` (`CLI(configurado)`, `ResumeArgs(sessão)`) e `provider.Watchable` (`WatchDirs`). O adaptador só monta argumentos; nunca executa.
- **ResumeService.Plan/Resume** (ordem das validações):
  1. chave válida;
  2. sessão no índice;
  3. provider habilitado;
  4. não é subagente;
  5. executável encontrado e executável;
  6. diretório de trabalho absoluto, existente e diretório (`WorkDir`, sem substituto);
  7. ID no formato do provider (UUID ou `ses_…`; nada que comece com `-`);
  8. terminal resolvido;
  9. terminal entrega o argv sem alterar (`Terminal.Check`).
  Sessão alterada há menos de 1 min (mtime do JSONL ou última mensagem) exige confirmação (`allowActive`). `Command` no plano é só exibição.
- **terminal**: o argv do emulador é montado item a item a partir do modelo (`{cmd}` vira executável + argumentos, `{dir}` o diretório); nada é concatenado numa string. O processo roda com `Setsid` (fechar o app não fecha a sessão) e é recolhido em segundo plano. **ptyxis expande `$VAR` nos argumentos** (medido): com ele, argumento ou diretório com `$` é recusado. tilix fica fora da detecção (o `-e` recebe uma string única).
- **Sincronização**:
  - `SyncService` serializa as execuções e junta os pedidos feitos durante uma execução. Há dois tipos:
    - **completa**: no startup, pelo botão "Atualizar índice" e na reconciliação a cada 5 min (1 min sem watcher); remove providers desabilitados. A reconciliação só avisa a interface se algo mudou.
    - **parcial**: só os providers que o watcher apontou, com `Throttle`.
  - O índice continua relendo a fonte inteira que mudou (Fase 2). Com `Throttle`, uma fonte de 8 MB ou mais relida há menos de 30 s é adiada (`Deferred`) e volta sozinha depois do intervalo, com no máximo uma nova tentativa agendada por provider.
  - Execuções sem efeito vão para o log em nível debug.
  - `index:updated` só é emitido quando algo mudou, ou ao fim de uma execução completa.
  - O relatório por provider é mesclado, então execuções parciais não apagam o estado dos outros providers.
- **Recuperação de alterações perdidas**:
  - `ErrEventOverflow` do inotify → indexação completa.
  - Pasta não monitorável (limite `max_user_watches`) → modo degradado, com reconciliação a cada minuto.
  - Raiz criada depois (provider instalado com o app aberto) → `Watcher.Refresh` na reconciliação.
  - Reabrir o app → indexação completa, que compara tamanho, mtime e assinatura de cada fonte.
- **Configurações**: `ConfigStore` guarda a config em uso e grava de forma atômica.
  - Executável e terminal valem na hora.
  - Diretório de dados e habilitação valem ao reiniciar (adaptadores e watcher são montados no início); a interface avisa.
  - Config inválido no disco não é sobrescrito pela interface.
- **Bindings**: todo método exportado de um serviço vira binding, então opções ficam no construtor (`NewWatchedSync`). `SyncService.Changed` é exportado porque o watcher (em `main`) o chama; chamado pelo frontend, só pede uma indexação.
- **macOS:** o fsnotify usa kqueue, que gasta um descritor de arquivo por pasta observada (no Linux, inotify gasta um *watch*). O Go (≥ 1.19) sobe o limite flexível de arquivos abertos ao máximo permitido ao iniciar (teste `TestFileLimitRaisedOnDarwin`). Se mesmo assim uma pasta não puder ser observada, o watcher entra no modo degradado já existente: registra uma vez e a reconciliação periódica (1 min) cobre as mudanças. Medição com um histórico real: roteiro `docs/manual-tests/0.2.0.md`.

## Analytics (Fase 8)

- Adaptadores extraem `domain.Usage` (Claude `claude/usage.go`, Codex `codex/usage.go`, OpenCode `opencode/usage.go`) e entregam por `provider.UsageSink` (interface opcional verificada por type assertion). O sink do indexador grava em `usage` (`SourceTx.AddUsage`), na mesma transação por fonte: idempotente, recuperável (falha = rollback) e determinístico. O log `indexação` ganha `usage=N`; divergências entre linhas (Claude) e soma ≠ acumulado (Codex) viram avisos sem conteúdo.
- `sqlite/usage_store.go`: CTE `d` com filtros aplicados na tabela (índice de `ts`), deduplicação entre sessões restrita às respostas repetidas, origem e sessão raiz; `UsageBreakdown` (uma consulta por provider × modelo × projeto × origem) somada em Go por `Sum`/`Group`; `UsageSeries`, `UsageSessions` (raízes, paginadas), `UsageSessionOrigins`, `UsageModels`, `UsageFirst`. Sem `visibleS`: consumo inclui tudo.
- `service/analytics.go` (`AnalyticsService`): `Overview`, `Usage`, `UsageSessions`, `SessionOrigins`, `Models`, `Activity`. A atividade chama as mesmas funções do `StatsService` (`series`, `totals`) e consultas novas no `stats_store.go` com o mesmo `where`/`visibleS` (`ActivityHeatmap`, `ActiveDays`, `TopProjectsBySessions`); `StatsFilter` ganhou `Project` e `BucketHour`.
- `service/period.go` (`resolveWindow`): janela compartilhada pelo Dashboard e pelo Analytics (hoje por hora, 7/30/90 dias, este mês, mês anterior, personalizado, tudo), com série anterior e totais anteriores pelo mesmo tempo decorrido.
- Frontend: `features/analytics` (`AnalyticsLayout` com abas e filtros na URL, `OverviewPage`, `UsagePage`, `ActivityPage`, `SessionsUsageTable`, `TokenStat` e `format.ts` com o tratamento de parcial/indisponível) e gráficos `StackedBar` e `Heatmap`. Detalhes em [ANALYTICS.md](ANALYTICS.md).

## Custos e padrões (Fase 9)

- **Preços e estimativas no `user.db`** (migration `user/0002_pricing`): `price_revisions` (revisões imutáveis) e `cost_estimates` (uma por resposta, chave `(provider, native_id, response_id)`). Ficam lá porque o `index.db` é descartável e a estimativa precisa preservar o preço usado. `sqlite/price_store.go`: revisões, `ReplaceRevision` e `CloseAndInsert` em transação, `SaveEstimates` (upsert em lote) e `EstimateUses`.
- **Relatórios entre bancos** (`sqlite/cost_store.go`): `CostStore` pega uma conexão dedicada do pool do índice (`db.Conn`), faz `ATTACH` do `user.db` com URI `?mode=ro` (caminho escapado como no `dsn`), roda a consulta e faz `DETACH`. Se o `DETACH` falhar, a conexão é descartada (`driver.ErrBadConn`). As consultas reusam a CTE `d` da Fase 8 (deduplicação, origem, raiz) com `LEFT JOIN u.cost_estimates`:
  - `Breakdown`: agente × modelo × projeto × intervalo × moeda, somado em Go;
  - `Sessions`;
  - `Uncalculable`.

  `IndexStore.UsageForEstimate` (sem deduplicação: toda cópia tem estimativa) e `UsageKeys` (com deduplicação, para a prévia do recálculo).
- **`service/cost.go` (`CostService`):**
  - `Refresh` grava estimativas que faltam, sem preço/sem dados ou com tokens mudados (com a revisão gravada);
  - `PreviewRecalculate`/`Recalculate`;
  - `Costs`, `CostSessions`, `Uncalculable`;
  - CRUD de preços (`AddPrice`, `EditPrice`, `NewPriceFrom`, `DisablePrice`, `ReactivatePrice`, `Prices`);
  - moeda padrão (`config.json`);
  - `ExportCSV`.

  Um mutex serializa `Refresh`, `Recalculate` e o CRUD. Depois de cada CRUD, o `Refresh` roda em segundo plano e emite `costs:updated`.
- **Sync → custos (`app.go`):** o callback da sync emite `index:updated` e roda `costSvc.Refresh` numa goroutine. Emite `costs:updated` só se alguma estimativa mudou. O frontend escuta os dois eventos no `AppLayout` (`touch()` → `dataVersion`).
- **`service/patterns.go` (`PatternsService`):** `SessionFacts` (com `lag()` sobre as mensagens com texto para a retomada), `SessionStarts`, `DailyActivity` e `LastActivityByProject` em `sqlite/pattern_store.go`, todos com o `where`/`visibleS` do Dashboard; agregados (sequências, distribuições *nearest-rank*, projetos, agentes) em Go. `AnalyticsService.activityWindow` é a janela compartilhada com a tela Atividade.
- **`service/export.go`:** CSV em formato longo (`encoding/csv`), `csvText` contra injeção de fórmula, diálogo de salvar do Wails injetado como `func(nome) (caminho, error)`.
- **Frontend:**
  - `features/analytics`: `CostsPage`, `CostSessionsTable` (por sessão e sem custo calculável), `RecalculatePanel` (prévia + confirmar), `PricesPage` + `PriceForm`, `PatternsPage`, `costFormat.ts`;
  - `lib/money.ts`: `formatMoney`/`roundDecimal` *half-even* sobre a string decimal, sem float;
  - `components/charts`: `CalendarGrid` e `Histogram`;
  - testes com **Vitest** (`npm test`, entra no `make check`).

## Estatísticas (Dashboard)

`service.StatsService` monta a tela inicial numa chamada (`Dashboard(StatsQuery{Period, Providers})`) e os projetos fixados da barra lateral (`PinnedProjects`: os 3 com mais mensagens em 30 dias).

- **Mensagem** é `domain.CountsAsMessage`: papel usuário ou assistente com bloco de texto não vazio. A mesma regra conta `message_count` nos adaptadores e preenche `messages.has_text` na indexação; passos só de ferramenta não contam como mensagem, mas suas chamadas entram em `messages.tools` (JSON com os nomes).
- Migration `0003_stats` adiciona as duas colunas, o índice de cobertura `messages (ts, has_text, session_id)` e esvazia `sync_state`: a próxima abertura relê todos os históricos para preencher as colunas.
- Consultas em `sqlite/stats_store.go`. O dia é o local: `(ts/1000 + offset)/86400`, com `offset` do fuso atual; semana começa na segunda. Os intervalos saem como inteiros do SQL e viram `AAAA-MM-DD` em Go (formatar data por linha dominava o custo). Períodos de 7, 30 e 90 dias usam dias; "Tudo" usa semanas até 2 anos de histórico e meses acima disso. Cada número tem o do período anterior de mesmo tamanho (exceto "Tudo").
- Sessões "abertas agora" e "Retomar última" usam a mesma heurística da retomada (`sessionActive`) sobre as 20 sessões mais recentes. Favoritas do período vêm de `user.db` (`FavoritesSince`), filtradas pelos providers e pela existência no índice.
- Meta: menos de 150 ms com o índice real (~150 MB); o teste de tempo não roda com `-race` nem `-short`.

## Frontend

- `HashRouter`: rotas `/`, `/sessions`, `/projects`, `/favorites`, `/search`, `/settings`.
- `lib/api.ts` é a única importação dos bindings. Normaliza slices Go nulos (`providersOf`).
- `lib/store.ts` (Zustand): status da aplicação, sessão selecionada, painel de detalhes e `dataVersion` (incrementado no evento `index:updated` e após favoritar/editar tags; listas e detalhes recarregam).
- `hooks/useAsync.ts`: chamada ao backend com loading/erro/recarregar; respostas de chamadas antigas são descartadas.
- `features/sessions`: `SessionList` (filtros por provider, ordem, tag, ocultas; "carregar mais"), `SessionRow`, `FavoriteButton` (otimista, reverte em erro), `TagEditor`, `ProviderTag`. `components/layout/DetailPanel` mostra os detalhes da sessão selecionada.
- Rotas: `/projects/view?path=…` lista as sessões de um projeto; `/session?id=…` é o viewer (carregado sob demanda, chunk separado); o painel de detalhes leva ao projeto, à sessão pai e à conversa ("Abrir conversa" ou duplo clique na linha).
- `features/viewer`: `useTranscript` (cursor, páginas, janela), `MessageRow` (mensagens e grupos de eventos de sistema), `Blocks` (um componente por tipo de bloco, igual para os três providers), `Markdown`.
- `components/layout`: `AppLayout` (sidebar, área principal e painel de detalhes), `PageHeader`, `EmptyState`, `StatusGate` (estados de carregando e erro).
- Atalhos globais (`hooks/useShortcuts.ts`): `Ctrl+K` busca (já na busca: volta o foco ao campo sem perder a consulta), `Ctrl+P` projetos, `Ctrl+,` configurações, `Ctrl+Enter` retoma a sessão selecionada. `Ctrl+F` ainda pendente.
- `features/resume`: `useResume` (Plan → confirmação se ativa → Resume), `ResumeConfirm` (diálogo próprio, nunca `window.confirm`) e `NoticeToast` (resultado da ação). O painel de detalhes mostra o comando exato ou o motivo de não poder retomar; o viewer tem "Retomar".
- Atualizações frequentes (watcher): listas recarregam em segundo plano sem esmaecer nem encolher ("carregar mais" aumenta o tamanho da página, até 1000, em vez de acumular páginas); falha numa recarga mantém a lista. A busca não recarrega sozinha: mostra "Índice atualizado · atualizar resultados".
- `features/settings`: formulários por provider (habilitado, diretório, executável) e do terminal (caminho, argumentos um por linha). A tela recarrega só depois de salvar (eventos de indexação não apagam o rascunho).
- `features/search/SearchPage`: estado na URL (`?q=&p=&project=&hidden=1`), então voltar do viewer restaura a busca; digitação com espera de 200 ms; a chamada anterior é cancelada (`CancellablePromise.cancel`) e respostas velhas, ignoradas; setas navegam entre resultados e Enter abre o primeiro; trecho renderizado como texto com `<mark>`; botão "Atualizar índice" chama `SyncService.Start`.
- Tema **Aurora**, dark fixo (`<html class="dark">`), sobre os tokens do shadcn: fundo `#060914`, acento ciano `#38e1f5`, secundário violeta `#a78bfa`, positivo `#5eead4`, negativo `#fb7185`; cor por agente (Claude `#fb923c`, Codex `#38e1f5`, OpenCode `#c084fc`) em `ProviderTag`, `ProviderPill` e gráficos. Fontes Inter e JetBrains Mono (variáveis, empacotadas via `@fontsource`, sem rede); ícones Lucide.
- Camadas (`index.css`): `.aurora-bg` (gradientes fixos atrás de tudo), `.glass` (translúcido com `backdrop-filter`, só em peças flutuantes ou fixas: sidebar, cabeçalho, painel de detalhes, diálogos, tooltips), `.surface` (cartão translúcido com borda superior iluminada e sombra, **sem** blur — é o que aparece muitas vezes, inclusive no viewer virtualizado), `.surface-sel` (selecionado com brilho ciano) e `.title-gradient`. Destaque de código com tema próprio na paleta (substitui o CSS do highlight.js).
- Gráficos em SVG feitos à mão (`components/charts`, sem biblioteca): `AreaChart` (áreas empilhadas por agente, período anterior tracejado, balão com detalhamento e "× a média"; mede a largura com `ResizeObserver`), `Sparkline`, `Donut`, `BarList`; `scale.ts` nunca produz `NaN` (séries vazias ou zeradas).
- `features/dashboard`: período (`Segmented`) e agentes (pílulas; o último ligado não desliga), quatro indicadores com variação e sparkline (Sessões e Mensagens trocam a métrica do gráfico), atividade por intervalo, "Por agente" (rosca + situação de detecção, antigo card de providers), rankings de projetos e ferramentas e tabela de recentes (Todas, Abertas, Favoritas). `summary.ts` gera a frase-resumo. Recarregar pelo `dataVersion` não esmaece a tela; só trocar filtro.
- Movimento com `motion` (`motion/react`). `MotionConfig reducedMotion="user"` e regra CSS `prefers-reduced-motion` desligam animações quando o sistema pede; laços contínuos animados em JS (`TextShimmer`, `BorderBeam`, `NumberTicker`) checam `useReducedMotion()`, porque o `MotionConfig` só cobre transform/layout. Componentes em `components/ui`, adaptados de bibliotecas MIT que o 21st.dev publica (o registry do 21st.dev exige login; o código veio dos registries públicos originais e foi lido antes de entrar):
  - Magic UI (`@dillionverma`): `NumberTicker` (indicadores do Dashboard), `BlurFade` (entrada de páginas, cards e linhas), `BorderBeam` (feixe na borda do card "Por agente" enquanto indexa).
  - motion-primitives (`@ibelick`): `TextShimmer` (textos de carregamento/indexação), `AnimatedBackground` (realce que desliza até a linha sob o mouse; reescrito com um único realce posicionado por delegação de eventos — o original re-renderiza a lista inteira a cada hover).
  - Próprios: `Skeleton`/`SkeletonRows` (carregamento), marcador ativo da sidebar com `layoutId`, painel de detalhes deslizando, estrela com mola, tags com entrada/saída, blocos do viewer abrindo com animação de altura.
- Regras de movimento: só entrada (navegar nunca espera animação); entrada escalonada limitada às 12 primeiras linhas; nada anima por linha no viewer virtualizado (linhas remontam ao rolar).

## Segurança e privacidade

- Históricos dos providers: somente leitura pelo SessionFlow. Executável de provider só roda quando o usuário pede a retomada, num terminal, com argv separado e sem shell; a detecção da CLI só procura o arquivo. As próprias CLIs gravam no histórico ao retomar (docs/PROVIDERS.md §Retomada). Bancos de terceiros passam por `sqliteread`; cópias ficam em `$XDG_CACHE_HOME/sessionflow/snapshots` (0700), são apagadas após a leitura e limpas no início do app.

- Logs: `logging.redact` troca valores de chaves como `content`, `text`, `prompt`, `message`, `input`, `output`, `args` e `result` por `[redacted]`. O logger do Wails tem **piso WARN**, porque em debug ele registra argumentos e retornos de bindings, que nas próximas fases conterão conversas.
- Conteúdo de sessão nunca é renderizado como HTML cru nem em iframe (advisory GHSA-2p5f-2gx8-mh8m do Wails).
- Sem rede: o app não faz requisições externas.

## Decisões

Ver [docs/adr/](adr/).
