# Modelo de domínio

Código: `internal/domain`. O modelo não depende dos formatos de nenhum provider. A normalização a partir de cada formato está em [discovery/00-relatorio.md §4](discovery/00-relatorio.md).

## Identidade

- **`SessionKey{Provider, NativeID}`**: identifica uma sessão entre providers. O mesmo ID nativo em providers diferentes gera chaves diferentes. Formato textual: `provider:idNativo` (`claude:1111…`, `opencode:ses_…`). `ParseSessionKey` aceita `:` dentro do ID.
- **Projeto**: identificado pelo caminho absoluto normalizado (`NormalizeProjectPath` = `filepath.Clean`, rejeita caminhos relativos). Sessões de providers diferentes no mesmo diretório caem no mesmo projeto. O nome exibido é `ProjectName` (último segmento).

## Entidades

| Tipo | Campos principais | Observações |
|---|---|---|
| `ProviderID` | `claude`, `codex`, `opencode` | `Valid()`, `DisplayName()` |
| `Project` | Path, Name, Providers, SessionCount, LastActivity | derivado das sessões |
| `Session` | Key, Parent?, ProjectPath, Title, FirstPrompt, Model, CreatedAt, UpdatedAt, MessageCount, SourceKind/Path, Archived, Automated, Favorite, Tags | `Parent` = subagente/subsessão; `Automated` = sem prompt humano |
| `Message` | Seq, NativeID, ParentID, Role, Timestamp, Blocks | `Seq` = ordem física na fonte |
| `Block` | Kind, Text, Language, ToolName, ToolCallID, ToolInput, IsError, Raw | `Raw` preserva o original em `metadata`/`unknown` |
| `Transcript` | Session, Messages, HasMore | página do viewer |

`Role`: `user`, `assistant`, `system` (metadados, contexto injetado, registros desconhecidos), `tool` (resultados de ferramenta).
`BlockKind`: `text`, `code`, `tool_call`, `tool_result`, `reasoning`, `image_ref` (`Language` = MIME), `file` (anexo: `Text` = nome, `Language` = MIME), `metadata`, `unknown`. Um tipo desconhecido **nunca é erro**: vira `unknown` e aparece recolhido no viewer.

## Persistência

| Tabela (`index.db`) | Chave | Uso |
|---|---|---|
| `projects` | `path` único | agrupamento |
| `sessions` | `(provider, native_id)` único | listagem e filtros |
| `messages` | `(session_id, seq)` único | `search_text` indexável |
| `messages_fts` | rowid = `messages.id` | busca FTS5 (`unicode61 remove_diacritics 2`) |
| `sync_state` | `(provider, source_path)` | cursor incremental opaco por fonte |

| Tabela (`user.db`) | Chave | Uso |
|---|---|---|
| `favorites` | `(provider, native_id)` | favoritos |
| `tags` | `name` único, sem diferenciar maiúsculas | tags |
| `session_tags` | `(tag_id, provider, native_id)` | associação |
| `price_revisions` | `id`; `price_key` agrupa as revisões de um preço | preços cadastrados pelo usuário (Fase 9) |
| `cost_estimates` | `(provider, native_id, response_id)` | estimativa de custo gravada por resposta (Fase 9) |

| `usage` | `(session_id, response_id)` | consumo de tokens por resposta/pedido; `NULL` = indisponível (Fase 8, [ANALYTICS.md](ANALYTICS.md)) |

Visibilidade: sessões com pai (subagentes/subsessões) ou `automated` ficam ocultas por padrão nas contagens e na listagem.

O `user.db` referencia sessões por `(provider, native_id)`, nunca pelo `id` do índice. Por isso favoritos e tags sobrevivem à reconstrução do índice (há teste para isso).

### Diferenças em relação à lista de tabelas do CLAUDE.md §8
- **`providers` e `settings`** → `config.json`. Precisam existir antes de abrir os bancos (nível de log, caminhos) e são editáveis à mão.
- **`content_blocks`** → não persistido. O viewer relê a fonte original sob demanda; o índice guarda só o texto pesquisável. Isso atende "não armazenar cópias integrais" (§8). Ver ADR 0003.

## Consumo (`domain.Usage`, Fase 8)

Um registro por resposta (Claude Code, OpenCode) ou pedido (Codex), entregue pelo adaptador já deduplicado através de `provider.UsageSink` (só o indexador implementa; o viewer não). Campos `Input`, `Output`, `CacheRead`, `CacheCreation`, `Reasoning`, `Total` são `*int64`: `nil` = o provider não informou (nunca zero). `Input` não inclui cache; `Reasoning` nunca entra em `Total`. `Derived` lista os campos calculados; `Status` é `complete` ou `partial` (`StatusFor` com os campos que o provider costuma informar).

**Regra de domínio:** consumo inclui sessões principais, subagentes e automáticas; atividade considera só sessões principais e não automáticas (as mesmas regras do Dashboard). Origem (`main_agent`, `subagent`, `automatic`) e associação com a sessão pai são derivadas da sessão. Detalhes e regras por provider em [ANALYTICS.md](ANALYTICS.md).

## Custos (`domain/cost.go`, Fase 9)

- **`PriceRevision`:** uma revisão de um preço. Tem agente, modelo (id ou prefixo `*`), moeda, unidade (1, 1000 ou 1.000.000 tokens) e preços de entrada, saída, cache lido e cache criado (decimal canônico; `nil` = sem preço). Tem também vigência `[ValidFrom, ValidTo)`, origem e observação. Revisão **imutável**: só `ReplacedBy` (editada), `DisabledAt` (desativada) e o fechamento da vigência (`ValidTo`/`ClosedAt`, quando ainda sem fim) mudam. `Current()` = nem substituída nem desativada. A vigência pode ser encerrada sem sucessor: fora de `[ValidFrom, ValidTo)` o preço não casa, e sem outro preço a estimativa fica `no_price`.
- **`Estimate`:** o custo de uma resposta com uma revisão. Tem tokens usados, custo por categoria e total em `Nanos` (`int64`, 10⁻⁹ da moeda; `nil` = não calculado, nunca zero), moeda, status (`complete`, `partial`, `no_price`, `insufficient`) e motivos fixos.
- **Funções puras:**
  - `ComputeEstimate`: fórmula, *half-even*, regras de status;
  - `MatchPrice`: agente, vigência, id exato antes do prefixo mais longo;
  - `ValidateRevision`: campos e sobreposição de vigência;
  - `ParseDecimal`: até 6 casas;
  - `ExpectedCostCategories`: o Codex não informa cache criado.
- **Ordem entre moedas** (`sqlite.CompareCostSums`): nunca compara valores de moedas diferentes; agrupa pela primeira moeda (código ISO em ordem alfabética), depois total da moeda e respostas.

As estimativas ficam no `user.db` (não reconstruíveis: preservam o preço usado) pela chave estável da resposta. Regras completas em [ANALYTICS.md §Custos](ANALYTICS.md#custos-fase-9).

## Padrões de atividade (Fase 9)

Sem tipo de domínio próprio: são agregados de sessões e mensagens com a regra de visibilidade do Dashboard (`service/patterns.go`, `sqlite/pattern_store.go`). Semântica de cada indicador em [ANALYTICS.md §Padrões](ANALYTICS.md#padrões-fase-9).
