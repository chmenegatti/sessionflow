# Fase 9 — Analytics: custos e padrões de atividade (design)

Data: 2026-11-23. Pedido: Fase 9 do roadmap (texto colado pelo usuário em 2026-11-23). Abordagem escolhida pelo usuário: **B** (estimativa gravada por resposta no `user.db`, recálculo só explícito).

## 1. Intenção

Dar uma **estimativa** do que o consumo de tokens custaria pelo preço de tabela que o próprio usuário cadastra, e mostrar **padrões de atividade** observáveis. Tudo local.

A estimativa precisa ser confiável e honesta:
- um número antigo não muda porque alguém editou um preço;
- dado faltando nunca vira zero calado;
- nada sugere que o valor é o que o usuário realmente paga, que pode ser uma assinatura.

### Objetivos
- **Preços:** tabela local de preços por modelo, com categorias opcionais, moeda, unidade e vigência. Dá para adicionar, editar (gerando nova revisão), desativar e consultar o histórico.
- **Estimativas:** uma estimativa gravada por resposta, contendo a revisão do preço usado, os tokens usados, o custo de cada categoria, a moeda, o status e a data do cálculo. Mudar um preço não altera estimativas existentes; só o **Recalcular** explícito altera.
- **Tela Custos:** total, por agente, modelo, projeto e sessão, evolução, comparação com o período anterior, composição por categoria, sessões sem custo calculável e qualidade da estimativa. Dá para navegar até a sessão de origem.
- **Tela Padrões:** métricas de atividade, rotuladas como tal: calendário, sessões iniciadas e retomadas, frequência e recorrência, projetos mais ativos, horários, duração observada, mensagens por sessão, intervalo entre sessões, vários agentes no mesmo projeto e comparação entre agentes.
- **Exportação CSV:** gerada localmente, das telas Custos e Padrões.

### Fora do escopo
- Cobrança real, APIs de billing, consulta de preços externos, conversão cambial, LLM e Control Center.
- Pendências que o ROADMAP listava "para a Fase 9" e que o pedido não cita ficam para depois. São elas:
  - vínculo de subagentes do Codex;
  - filtro por data na lista de sessões;
  - fuso com horário de verão por registro;
  - reasoning do OpenCode.

  O ROADMAP passa a listá-las como pendências gerais.

### Anti-objetivos (falharia mesmo funcionando)
- Custo antigo que muda sozinho depois de editar um preço.
- Somar moedas diferentes.
- Mostrar `R$ 0,00` para resposta sem preço ou sem tokens.
- Qualquer texto que chame indicador de atividade de produtividade, eficiência, qualidade ou tempo de trabalho.
- Valor monetário em `float64` armazenado ou somado.

## 2. Dados de partida (Fase 8)

- **Origem do consumo:** a tabela `usage` do `index.db` tem um registro por resposta ou pedido, com tokens anuláveis (`NULL` = indisponível). A entrada já vem sem cache, inclusive a do Codex, onde é derivada. Por isso a fórmula de custo se aplica direto.
- **Deduplicação entre sessões:** é feita **na consulta**, pela CTE de `UsageFilter.base()`. Os relatórios de custo precisam usar a mesma CTE para não contar duas vezes.
- **Dois bancos com papéis diferentes:** o `index.db` pode ser apagado e reconstruído; o `user.db` nunca. Preços e estimativas ficam no `user.db`, referenciando a resposta pela chave estável `(provider, native_id da sessão, response_id)`, como já fazem favoritos e tags.
- **Atividade:** as consultas são as do Dashboard (`stats_store.go`), com a regra de visibilidade `parent_native_id IS NULL AND automated = 0`. Os padrões de atividade seguem a mesma regra.

## 3. Preços

### 3.1 Modelo
Um **preço** vale para um agente e um modelo. Cada edição cria uma **revisão**, e a revisão nunca é alterada depois de criada, exceto para ser marcada como substituída ou desativada.

Migration do `user.db` `0002_pricing.sql`:

```
price_revisions(
  id            INTEGER PRIMARY KEY,
  price_key     TEXT NOT NULL,     -- uuid estável do preço; todas as revisões compartilham
  provider      TEXT NOT NULL,     -- claude | codex | opencode
  model         TEXT NOT NULL,     -- id exato, ou prefixo terminado em '*'
  currency      TEXT NOT NULL,     -- ISO 4217, 3 letras maiúsculas
  unit_tokens   INTEGER NOT NULL,  -- 1, 1000 ou 1000000
  input         TEXT,              -- decimal canônico; NULL = categoria sem preço
  output        TEXT,
  cache_read    TEXT,
  cache_creation TEXT,
  valid_from    INTEGER NOT NULL,  -- ms UTC, meia-noite local do dia escolhido
  valid_to      INTEGER,           -- ms UTC exclusivo; NULL = sem fim
  source        TEXT NOT NULL DEFAULT '',  -- origem informada pelo usuário (ex.: "página de preços, 2026-11")
  note          TEXT NOT NULL DEFAULT '',
  created_at    INTEGER NOT NULL,
  replaced_by   INTEGER REFERENCES price_revisions(id),  -- revisão que a substituiu
  disabled_at   INTEGER            -- desativado: não vale para cálculos novos
)
```

- **Preços** são guardados como texto decimal canônico, com até 6 casas, sem expoente. `0` significa gratuito; `NULL` significa sem preço para a categoria.
- **Reasoning não tem preço:** já está dentro da saída, ou não se sabe (docs/ANALYTICS.md).
- **Revisão vigente** é a que não foi substituída nem desativada.
- **Editar** cria uma revisão nova com o mesmo `price_key` e grava `replaced_by` na anterior, numa transação só. As estimativas continuam apontando para a revisão antiga.
- **Desativar** grava `disabled_at` na revisão vigente. Dá para reativar, o que cria uma revisão nova.
- **Histórico de um preço** são todas as revisões com o mesmo `price_key`, em ordem.
- **Mudança de preço a partir de uma data** é um preço novo, não uma edição. Ele usa o mesmo agente e modelo, com `valid_from` posterior, e a interface oferece "novo preço a partir de…", que fecha o `valid_to` do anterior. A edição corrige um valor; a vigência registra uma mudança no tempo.
- **Fechar a vigência** é a única alteração no lugar permitida: grava `valid_to` numa revisão que ainda não tinha fim. Isso não cria revisão nova nem aparece como edição no histórico.
  - Os valores da revisão (preços, moeda, unidade) nunca mudam, então nenhum custo gravado muda.
  - Estimativas cujo horário cai depois do novo `valid_to` mantêm a revisão gravada até o **Recalcular**, mesma regra de qualquer mudança de preço. A tela avisa quantas são.
  - Para registrar o fechamento no histórico, a migration ganha a coluna `closed_at INTEGER`.
- **Validação no Go:**
  - moeda com 3 letras;
  - unidade em {1, 1000, 1000000};
  - decimais ≥ 0 com no máximo 6 casas;
  - pelo menos uma categoria com preço;
  - `valid_to > valid_from`;
  - nenhuma sobreposição de vigência entre preços vigentes do mesmo `(provider, model)`.

  Erro de validação vira mensagem em português, como o `ErrInvalidFilter`.

### 3.2 Qual preço vale para uma resposta
- **Agente:** igual ao da sessão.
- **Modelo:** o id exato vence; depois, o prefixo `*` mais longo que casar. A comparação não diferencia maiúsculas.
- **Data:** o `ts` da resposta precisa estar em `[valid_from, valid_to)`.
- **Revisão:** só revisões vigentes e não desativadas valem para cálculos novos.
- **Resposta sem `ts`:** fica sem estimativa calculável, com status `insufficient` e motivo `sem horário`. Não há data para escolher a vigência.

### 3.3 Tabela inicial
Começa **vazia**, sem preço embutido. Toda tela de custos sem preço cadastrado leva para o cadastro. O usuário escolhe a **moeda padrão**, que só preenche o formulário; o padrão é BRL. Ela fica no `config.json`, ao lado das outras preferências.

## 4. Estimativas

### 4.1 Tabela
```
cost_estimates(
  provider, native_id, response_id  TEXT NOT NULL,   -- PK composta
  revision_id     INTEGER REFERENCES price_revisions(id),  -- NULL = sem preço / sem dados
  currency        TEXT,
  unit_tokens     INTEGER,
  input_tokens, output_tokens, cache_read_tokens, cache_creation_tokens  INTEGER,  -- tokens usados (cópia)
  input_cost, output_cost, cache_read_cost, cache_creation_cost  INTEGER,  -- nano-unidades da moeda; NULL = não calculado
  total_cost      INTEGER,            -- soma dos componentes calculados; NULL se nenhum
  status          TEXT NOT NULL,      -- complete | partial | no_price | insufficient
  reasons         TEXT NOT NULL DEFAULT '',  -- lista curta separada por ';' (ex.: "cache criado sem preço")
  usage_ts        INTEGER,
  calculated_at   INTEGER NOT NULL,
  PRIMARY KEY (provider, native_id, response_id)
)
```

Há uma estimativa por registro de `usage`, inclusive das cópias que a deduplicação descarta. A deduplicação acontece na consulta do relatório, como no consumo.

### 4.2 Fórmula e aritmética
```
custo_categoria = tokens_categoria × preço_categoria / unit_tokens
custo_total     = soma das categorias calculadas
```

As categorias são entrada (sem cache), saída, cache lido e cache criado. Reasoning não entra.

- **Unidade guardada:** nano-unidades da moeda (10⁻⁹), em `INTEGER`. Isso comporta até ≈ 9,2 bilhões na moeda, e as somas em SQL são exatas.
- **Conta:** feita em Go com `math/big.Int`, para não haver overflow nem `float64`. O preço é lido como inteiro em micro-unidades (6 casas), então `nanos = tokens × preço_micro × 1000 / unit_tokens`, arredondado *half-even* no nano. Sem dependência nova.
- **Exibição:** o arredondamento é só na interface e no CSV, *half-even*. São 2 casas por padrão, e até 6 quando o valor é menor que 0,01 e diferente de zero. O CSV traz o valor com 9 casas, sem arredondar.

### 4.3 Status
| status | quando |
|---|---|
| `complete` | todas as categorias que o agente informa e que têm tokens foram calculadas |
| `partial` | algum custo foi calculado, mas falta parte: categoria com tokens e sem preço na revisão, ou categoria que o agente costuma informar e veio indisponível (`NULL`) |
| `no_price` | há tokens, mas nenhum preço vigente casa com agente, modelo e data |
| `insufficient` | sem horário, sem modelo ou sem nenhum token informado |

- Categoria que o agente **nunca** informa (cache criado do Codex) não torna a estimativa parcial; é a mesma regra da Fase 8.
- Categoria com **0 tokens** custa 0, mesmo sem preço.
- Os motivos são textos curtos fixos, sem conteúdo de conversa.

### 4.4 Quando calcular
- **Depois de cada sincronização** (o mesmo ponto que emite `index:updated`), o `CostService.Refresh` compara as linhas de `usage` com `cost_estimates`:
  - **sem estimativa:** calcula com o preço vigente;
  - **status `no_price` ou `insufficient`:** recalcula sozinho. Não há preço guardado a preservar, e o usuário espera ver o custo assim que cadastra o preço que faltava;
  - **tokens diferentes da cópia gravada** (resposta ainda crescendo numa sessão ativa): recalcula **com a mesma revisão** que já estava gravada. O preço fica preservado e só os tokens se atualizam. Se aquela revisão estiver desativada, continua valendo para essa estimativa;
  - **estimativas `complete` e `partial` com tokens iguais:** nunca mudam sozinhas.
- **Também depois de mudar preços:** cadastrar, editar, reativar ou fechar a vigência de um preço dispara o `Refresh` na hora, em segundo plano. Quando ele termina, o evento `costs:updated` faz a tela recarregar.
  - `Refresh`, `Recalculate` e o CRUD de preços passam por um único mutex do `CostService`, então nunca rodam ao mesmo tempo.
  - Uma sync que termina durante um `Recalculate` espera.
- **Recalcular (explícito):** recebe o recorte da tela (período, agentes, projeto, modelo) e recalcula com os preços vigentes **todas** as estimativas das linhas de `usage` do recorte.
  - O recorte usa o filtro **sem** a deduplicação, para que as cópias descartadas também recebam o preço novo. Assim, se a cópia vencedora mudar depois (a sessão principal some do índice), o relatório não volta ao preço antigo.
  - A prévia mostra quantas estimativas mudam (todas as linhas) e a diferença de total, calculada como no relatório, com deduplicação.
  - Executa numa transação.
- **Desempenho:** a comparação lê `usage` (hoje ~32 mil linhas) e `cost_estimates` em Go. Meta: < 300 ms, em segundo plano, sem travar a interface. Se o volume crescer, trocar por comparação incremental por sessão alterada (pendência registrada, não feita agora).
- **Sessão que some do índice:** a estimativa fica no `user.db` e não aparece nos relatórios, porque eles juntam com o índice. Se a sessão voltar, a estimativa volta com o preço preservado.

### 4.5 Relatórios (consulta entre bancos)
- Os relatórios de custo rodam numa conexão dedicada do `index.db` (`db.Conn`) com `ATTACH DATABASE '<user.db>?mode=ro' AS u`. Reusam a CTE de deduplicação da Fase 8 e fazem `LEFT JOIN u.cost_estimates` pela chave estável. Ao fim, `DETACH` e a conexão volta ao pool.
- **Agrupamentos:** agente, modelo, projeto, sessão raiz (mesma consolidação de descendentes da Fase 8), intervalo (hora, dia, semana ou mês, como a `Window`) e categoria. Sempre separados por **moeda**.
- **Cada grupo devolve:**
  - soma em nanos por categoria e total;
  - contagem de registros por status;
  - registros com custo calculado.
- **Comparação com o período anterior:** a mesma regra de tempo decorrido da Fase 8 (`Window.PrevTotals*`).
- **Cobertura:** o total mostra "estimativa sobre N% das respostas" quando houver `no_price`, `insufficient` ou `partial`.

## 5. Padrões de atividade

Todos seguem a regra de visibilidade do Dashboard: sessões principais e não automáticas. Usam os mesmos filtros de período, agente e projeto, e o fuso local. Cada indicador tem semântica fixa, documentada em `docs/ANALYTICS.md`:

| Indicador | Definição |
|---|---|
| Sessões iniciadas | sessões com `created_at` no período, por intervalo, com comparação ao anterior |
| Sessões retomadas | sessões com uma mensagem com texto (`has_text`, regra do Dashboard) no período que veio depois de uma pausa ≥ **30 min** desde a mensagem **com texto** anterior da mesma sessão. A mensagem anterior pode estar antes do período, e a sessão pode ter sido criada no período. Ferramentas e mensagens de sistema sem texto não medem pausa. Cada sessão conta uma vez por período, no intervalo da primeira retomada. A pausa é uma constante no código, não uma opção da interface |
| Frequência | dias com atividade, semanas com atividade, sessões por dia ativo, sequência atual e maior sequência de dias seguidos |
| Recorrência | projetos com sessões em ≥ 2 dias distintos no período; dias desde a última sessão por projeto |

**Data de referência** de "sequência atual" e "dias desde a última sessão": o fim do período limitado a hoje, ou seja `min(agora, fim do período)`. Em "Mês anterior", a referência é o último dia daquele mês.
| Projetos mais ativos | por sessões, com mensagens e dias ativos ao lado; top 10 |
| Dia da semana × hora | o mapa existente da Atividade, sem refazer |
| Calendário | uma célula por dia (estilo grade anual), com sessões por dia, no período escolhido, até 1 ano |
| Duração observada | da primeira à última mensagem com horário da sessão. Mostra mediana, p90 e distribuição em faixas (< 5 min, 5–30 min, 30 min–2 h, > 2 h). Rotulada "tempo entre a primeira e a última mensagem, não tempo de trabalho" |
| Mensagens por sessão | mensagens com texto (regra do Dashboard); mediana, p90 e distribuição |
| Intervalo entre sessões | entre os inícios de sessões consecutivas (globais, no filtro); mediana e distribuição |
| Vários agentes no mesmo projeto | projetos com sessões de ≥ 2 agentes no período, listando os agentes e as sessões de cada um |
| Comparação entre agentes | por agente: sessões, mensagens, mediana de duração e dias ativos |

**Fixo nas telas:** "Métricas de atividade observada nos históricos. Não medem produtividade, qualidade, esforço ou valor entregue."

**Fora:** tempo ativo, score, ranking de eficiência e qualquer cruzamento de tokens ou custo com produtividade.

## 6. Interface

- **Abas do Analytics:** Visão geral, Consumo, Atividade, **Custos** e **Padrões**. Os filtros são compartilhados pela URL; Custos também tem o filtro de modelo.
- **Custos** (`/analytics/costs`):
  - selo fixo **"Estimativa"**;
  - aviso: "Calculado com os preços que você cadastrou. Não é o valor cobrado; planos por assinatura não são cobrados por token";
  - total por moeda, com variação e cobertura;
  - indicadores de qualidade: respostas completas, parciais, sem preço e sem dados;
  - evolução por intervalo;
  - por agente, modelo e projeto;
  - composição por categoria;
  - tabela de sessões paginada, com link para o viewer;
  - lista "Sessões sem custo calculável", com motivo e link;
  - botões **Recalcular** e **Exportar CSV**.
- **Preços** (`/analytics/costs/prices`):
  - lista dos preços vigentes e desativados;
  - formulário para adicionar e editar (edição = nova revisão; a tela avisa que estimativas antigas não mudam);
  - "novo preço a partir de…";
  - desativar e reativar;
  - histórico de revisões com o número de estimativas que usam cada revisão.
- **Padrões** (`/analytics/patterns`): os indicadores da §5, o aviso fixo e **Exportar CSV**.
- **Valores monetários:** mostrados com `Intl.NumberFormat` na moeda do grupo. Sem preço aparece "sem preço", nunca `0`.

## 7. Exportação CSV

- O backend gera o CSV a partir do recorte atual e grava pelo diálogo nativo de salvar do Wails v3. Nada sai da máquina.
- **Formato longo**, uma linha por grupo e métrica: `periodo_inicio, periodo_fim, agente, modelo, projeto, sessao, metrica, valor, moeda, status, cobertura`.
  - **Custos:** uma linha por (agente × modelo × projeto × moeda) com as métricas `custo_total`, `custo_entrada`, `custo_saida`, `custo_cache_lido`, `custo_cache_criado`, `respostas`, `respostas_sem_preco` e similares.
  - **Padrões:** uma linha por indicador e grupo.
- **Formatação:** UTF-8, separador vírgula, ponto decimal, datas ISO 8601. Projeto é o caminho; agente é o id.
- **Proteção contra injeção de fórmula:** célula de texto começando com `=`, `+`, `-` ou `@` é prefixada com `'`.
- Nenhum texto de conversa: o título da sessão não entra, só `provider:native_id`.

## 8. Backend

- **`internal/domain/cost.go`:** `Money` (nanos int64 + moeda), parse e formato de decimal canônico, `PriceRevision`, `Estimate`, `EstimateStatus` e a função pura de cálculo. Sem dependência de banco.
- **`internal/repository/sqlite/price_store.go`:** CRUD de revisões e estimativas no `user.db`.
- **`internal/repository/sqlite/cost_store.go`:** relatórios com `ATTACH`.
- **`internal/repository/sqlite/pattern_store.go`:** consultas dos padrões no `index.db`.
- **`internal/service/cost.go`:** `CostService`, com `Refresh`, `Recalculate`, os relatórios, CRUD de preços e `ExportCSV`.
- **`internal/service/patterns.go`:** `PatternsService`.
- **Ligação:** wiring em `app.go`; `Refresh` é encadeado no fim de cada sync.
- **Contratos da Fase 8:** não mudam. `AnalyticsQuery` e `Window` são reusados.

## 9. Testes

- **Fórmula:** cada categoria, unidades 1, 1.000 e 1.000.000, preço 0, preço `NULL` com tokens > 0 e com tokens 0, e tokens `NULL`.
- **Precisão:** valores que somam errado em `float64` (ex.: 0,1 + 0,2), *half-even* no nano e na exibição, tokens em 10⁹ × preço máximo sem overflow.
- **Status:** os quatro, mais as regras de categoria nunca informada (Codex) e sem horário.
- **Vigência:** limites `[valid_from, valid_to)`, escolha por data, prefixo `*` contra id exato, sobreposição rejeitada.
- **Histórico:**
  - editar um preço não muda a estimativa gravada;
  - `Recalcular` muda;
  - `no_price` vira calculado sozinho quando o preço aparece;
  - tokens crescendo recalcula com a mesma revisão;
  - revisão desativada continua nas estimativas antigas.
- **Moedas:** relatórios nunca somam moedas diferentes.
- **Relatório:** deduplicação igual à do consumo (a resposta copiada no subagente custa uma vez); reconciliação por agente, modelo e sessão contra o total; comparação com o período anterior.
- **Padrões:** cada indicador da §5 sobre fixtures com horários conhecidos (pausa de 29 contra 30 min, sequência de dias, projeto com dois agentes); paridade de "sessões iniciadas" e "mensagens" com o Dashboard.
- **CSV:** cabeçalho, escape de vírgula e aspas, prefixo contra fórmula, decimal com ponto e 9 casas, ausência de título e de texto de conversa.
- **Frontend:** formatação de moeda e cobertura. Entra o **Vitest** (dependência só de desenvolvimento), que já era pendência registrada, com os testes de `format.ts` que hoje são scripts avulsos.

## 10. Documentação
Fórmula, status, vigência, histórico, recálculo e limitações em `docs/ANALYTICS.md`. Entidades em `docs/DOMAIN.md`. Pacotes, `ATTACH` e o fluxo de sync → refresh em `docs/ARCHITECTURE.md`. Fase 9 e pendências em `docs/ROADMAP.md`.

## 11. Limitações conhecidas (vão para a doc)
- **Cache criado do Claude:** um preço só. O Claude cobra 5 min e 1 h de forma diferente, e o histórico não separa.
- **Descontos não modelados:** lote, faixas por volume e contexto longo com preço diferente ficam de fora.
- **Assinaturas:** a estimativa não representa o que o usuário paga.
- **Sem horário:** o registro fica sem estimativa.
- **Volume da comparação:** a comparação depois da sync é completa (O(n)); fica incremental quando o volume pedir.

## 12. Decisões do designer (para o usuário conferir)
1. **Tabela inicial vazia** e moeda padrão BRL no `config.json`.
2. **Editar ≠ mudar a partir de uma data:** a edição cria uma revisão do mesmo preço; uma mudança de preço no tempo é um preço novo com vigência.
3. **`no_price` e `insufficient` recalculam sozinhos;** tokens crescendo recalculam com a revisão já gravada.
4. **Dinheiro:** nanos em `INTEGER` e conta com `math/big`, sem biblioteca decimal nova.
5. **Retomada:** pausa ≥ 30 min dentro da mesma sessão.
6. **Padrões** como aba nova, sem mexer na aba Atividade (que tem paridade com o Dashboard).
7. **Vitest** entra nesta fase.
8. **Sem horário** dá `insufficient`.
9. **Fechar a vigência** altera só o `valid_to` (com `closed_at`), sem criar revisão nova.
10. **CRUD de preço dispara o `Refresh` na hora,** e um mutex único serializa `Refresh`, `Recalculate` e o CRUD.
11. **Recalcular inclui as cópias descartadas pela deduplicação.**
12. **Retomada:** a pausa é medida entre mensagens com texto, e a anterior pode estar fora do período.
13. **Data de referência das sequências** é `min(agora, fim do período)`.
