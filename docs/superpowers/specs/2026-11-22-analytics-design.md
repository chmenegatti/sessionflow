# Fase 8 — Analytics: consumo e atividade (design)

Data: 2026-11-22. Pedido: Fase 8 do roadmap e a revisão de escopo que a seguiu. Abordagem escolhida: **A** (registros de consumo extraídos pelos adaptadores, tabela própria no índice, agregação em SQL).

## 1. Intenção

Saber quanto cada agente consumiu de tokens, onde (provider, modelo, projeto, sessão, origem) e quando, e como os agentes são usados ao longo do tempo. Tudo local, a partir dos históricos, sem custo em dinheiro e sem estimar o que os dados não trazem.

A pessoa que usa precisa confiar no número: um total errado (dupla contagem, ausência virando zero) é pior que nenhum total.

### Objetivos
- Seção **Analytics** própria na barra lateral, com três telas: **Overview** (consumo), **Usage** (detalhamento do consumo), **Activity** (atividade).
- Contabilização correta por agente, sem dupla contagem (repetições na mesma resposta, entre arquivos, acumulados, subagentes).
- Qualidade do dado visível: completo, parcial, indisponível; derivado marcado como derivado.
- Filtros compartilhados entre as três telas: período, provider, projeto.
- Mesmos números de atividade no Dashboard e no Analytics (mesmas consultas).

### Fora do escopo (não fazer)
- Custos e preços. Control Center. APIs externas. LLM ou embeddings.
- Estimar tokens pelo tamanho do texto.
- Rótulos de produtividade ou qualidade de trabalho.
- Alterar o Session Manager (listas, viewer, busca, retomada) além do necessário para compartilhar consultas.

### Anti-objetivos (falharia mesmo funcionando)
- Mostrar `0` onde o provider não informa o campo.
- Somar o acumulado do Codex aos pedidos, ou o resumo de tokens do subagente que aparece no transcript principal (`toolUseResult.totalTokens`) ao consumo do próprio subagente.
- Atividade com números diferentes do Dashboard para o mesmo filtro.

## 2. O que cada provider informa (verificado nos históricos locais)

| Campo normalizado | Claude Code | Codex | OpenCode |
|---|---|---|---|
| unidade de contagem | resposta (`message.id`) | pedido (`event_msg/token_count`) | resposta (mensagem `assistant`) |
| `input_tokens` (sem cache) | informado (`input_tokens`; não inclui cache: mediana 2 contra 240 mil de cache lido) | **derivado**: `input_tokens − cached_input_tokens` (o Codex inclui o cache na entrada) | informado (`tokens.input`) |
| `output_tokens` | informado | informado (já inclui reasoning) | informado |
| `cache_read_tokens` | informado (`cache_read_input_tokens`) | informado (`cached_input_tokens`) | informado (`tokens.cache.read`) |
| `cache_creation_tokens` | informado (`cache_creation_input_tokens`) | **indisponível** | informado (`tokens.cache.write`) |
| `reasoning_tokens` | **indisponível** (pensamento vem dentro da saída, sem campo) | informado (`reasoning_output_tokens`, contido na saída) | informado (`tokens.reasoning`; inclusão na saída não verificável localmente, todos os registros têm 0) |
| `total_tokens` | **derivado**: input + output + cache_read + cache_creation | informado (`total_tokens` = input + output, conferido) | informado (`tokens.total`, = input + output + cache em 145/145); se ausente, ver §3.3 |
| modelo | `message.model` | `turn_context.model` vigente no pedido | `modelID` da mensagem |

**Reasoning nunca é somado ao total.** É sempre exibido como "dos quais reasoning", parte da saída quando o provider diz que é (Codex) e como categoria à parte quando não se sabe (OpenCode).

## 3. Regras de contabilização

### 3.1 Claude Code
- Uma resposta ocupa várias linhas (`apiBlockIndex`). Numa sessão real, 1.912 de 2.803 respostas têm várias linhas, **todas com `usage` idêntico**; as outras 891 têm linha única. **Identidade = `message.id`**; sem ele, `requestId`; sem nenhum, a resposta não é deduplicada e usa o `uuid` da linha. Nunca deduplicar por igualdade de valores.
- Dentro de um arquivo, cada identidade gera um registro. Vale a **última** linha da identidade (estado final da resposta); se os valores divergirem entre linhas, a divergência é contada no log (sem conteúdo). *(Decisão do implementador: nos dados observados nunca divergem.)*
- Entre arquivos: a mesma resposta aparece no principal e num subagente (4 de 878 observadas). Resolvido na consulta (§4.3).
- `toolUseResult` da ferramenta Task (77 ocorrências com `totalTokens`/`usage`) **não** é contabilizado: é o resumo do subagente, cujas respostas já estão no arquivo dele.
- Modelo `<synthetic>` (mensagens geradas pelo próprio Claude Code) não tem consumo real: o registro é descartado se todos os campos vierem 0 com esse modelo.

### 3.2 Codex
- Cada `token_count` traz `last_token_usage` (o pedido) e `total_token_usage` (acumulado da sessão). **Contabiliza-se `last_token_usage`.** O acumulado nunca é somado.
- Não há ID nos eventos. **Identidade = (sessão, `total_token_usage.total_tokens`)**: evento cujo acumulado não avança em relação ao último aceito é repetição e é ignorado (2 de 87 na sessão local).
- `info` nulo: evento ignorado (não é pedido).
- **Reconciliação**: ao fim do arquivo, a soma de `last.total_tokens` aceitos deve igualar o último acumulado. Diferença → aviso no log de indexação (sem conteúdo) e na tabela de verificação dos testes; não vira selo na interface nem coluna nova. Na sessão local bate: 5.512.021. *(Decisão do implementador.)*
- Se o acumulado **diminuir** (sessão retomada que reinicia a contagem), o evento é aceito e a referência passa a ser o novo acumulado.
- Modelo: o do `turn_context` mais recente antes do evento.

### 3.3 OpenCode
- Um registro por mensagem `assistant` com `tokens`. Partes `step-finish` **não** são contabilizadas (são as mesmas contas por passo).
- `total` informado é usado como está. Ausente (2 de 147): derivar `input + output + cache.read + cache.write` **somente se `reasoning` = 0**; com reasoning > 0 e sem total, `total_tokens` fica indisponível (não se sabe se reasoning está dentro da saída).

### 3.4 Consumo × atividade (regra de domínio, decidida pelo usuário)

**Consumo de tokens** é a utilização técnica real e **inclui tudo**: sessões principais, subagentes e automáticas. Classificação ou visibilidade no Session Manager nunca exclui consumo.

**Atividade** segue **exatamente** as regras atuais do Dashboard: só sessões principais (`parent_native_id IS NULL`) e não automáticas (`automated = 0`), e as mensagens delas. Mesmas consultas, filtros e regras (`visibleS`). Sem controle para incluir subagentes ou automáticas nesta fase.

**Origem** de cada sessão (para o consumo):
- `subagent`: tem `parent_native_id` (mesmo que também seja automática).
- `automatic`: `automated = 1` e sem pai.
- `main_agent`: as demais.

**Associação**: uma sessão subordinada é *associada* quando a sessão pai (`provider`, `parent_native_id`) existe no índice. Sem pai no índice, é **não associada**: continua no consumo global e aparece identificada como tal.

**Tabela por sessão**: cada linha é uma sessão raiz (principal ou automática sem pai) com o consumo consolidado dos descendentes associados; ao expandir, detalhamento por origem (`main_agent`, `subagent`, `automatic`) que reconcilia com a linha. Subordinadas não associadas viram linhas próprias marcadas "não associada". Sessões automáticas sem pai são raízes próprias (origem `automatic`).

**Codex**: o vínculo com a sessão pai (`parent_thread_id`) **não** é implementado nesta fase (formato não confirmado; só há uma sessão local, e mudaria as listas do Session Manager). Sessões do Codex ficam com a origem que a classificação atual dá (`automatic` se `automated`, senão `main_agent`); limitação documentada. *(Decisão do implementador.)*

**Transparência**: as três telas exibem "Consumo de tokens inclui subagentes e sessões automáticas. Atividade considera somente sessões principais e não automáticas."

### 3.5 Qualidade
Por registro, `measurement_status`:
- `complete`: todos os campos que o provider informa (tabela §2) vieram.
- `partial`: algum desses campos faltou.
- Campo indisponível por natureza do provider (ex.: reasoning do Claude) **não** torna o registro parcial.

Por sessão: `unavailable` quando há respostas do agente e nenhum registro de consumo.

Por campo: `NULL` = indisponível. Campos derivados ficam registrados no próprio registro (`derived`), para a interface marcar.

Agregados: a soma de um campo usa só registros que o têm, e devolve a **cobertura** (registros com o campo / registros no recorte). A interface mostra "parcial" quando a cobertura é < 100% e "indisponível" quando é 0%, nunca `0`.

## 4. Modelo de dados

### 4.1 Domínio
`domain.Usage`: identidade da resposta (`ResponseID`), sessão (`SessionKey`), `Timestamp`, `Model`, campos `*int64` (nil = indisponível), `Derived` (quais campos foram derivados), `Status` (`complete`/`partial`), `Seq` (posição na fonte, para determinismo).

Os adaptadores entregam `Usage` por uma interface opcional do sink (`UsageSink`, verificada por type assertion). O viewer não implementa e não muda; só o indexador grava.

### 4.2 Tabela (migration `0004_usage`, no `index.db`)
```
usage (
  id, session_id → sessions(id) ON DELETE CASCADE,
  response_id TEXT NOT NULL, seq INTEGER NOT NULL,
  ts INTEGER,                      -- ms UTC; NULL fica fora das séries, entra nos totais sem período
  model TEXT NOT NULL DEFAULT '',
  input_tokens, output_tokens, cache_read_tokens, cache_creation_tokens,
  reasoning_tokens, total_tokens INTEGER,   -- NULL = indisponível
  derived TEXT NOT NULL DEFAULT '',          -- ex.: "input,total"
  status TEXT NOT NULL,                      -- complete | partial
  UNIQUE (session_id, response_id)
)
índices: (ts, session_id); (response_id)
```
- Granularidade: **um registro por resposta/pedido**. Por sessão perderia a série temporal e a troca de modelo; por linha repetiria o Claude.
- `UNIQUE (session_id, response_id)` garante idempotência dentro de uma sessão. A migration também esvazia `sync_state` (releitura completa, como a 0003).
- Projeto, provider e origem vêm do `JOIN sessions` (não duplicados na tabela).

### 4.3 Deduplicação entre arquivos (na consulta)
A mesma `response_id` em sessões diferentes do mesmo provider conta **uma vez**: vale a cópia da sessão principal (`main_agent`); entre iguais, a de menor `ts`, depois a de menor `session_id`. Feito na consulta, a regra não depende da ordem de indexação e sobrevive à releitura de um arquivo só.

## 5. Indexação e releitura
- Usa o fluxo atual: uma transação por fonte, que apaga a sessão da fonte (cascata leva mensagens e consumo) e regrava tudo; `sync_state` só é gravado no commit. Isso dá **idempotência** (reexecutar gera as mesmas linhas), **recuperação** (interrupção = rollback; a fonte é relida na próxima varredura) e **determinismo** (ordem física da fonte).
- Arquivo incompleto (última linha parcial): ignorada como hoje; o registro entra quando a linha completar.
- Leitura em streaming, uma fonte por vez; nada de carregar todos os arquivos.
- O log de indexação por fonte ganha a contagem de registros de consumo e de repetições descartadas (sem conteúdo). Erros por arquivo e provider seguem o log atual.

## 6. Backend: módulo analytics
- `internal/service/analytics.go` (`AnalyticsService`) + consultas em `internal/repository/sqlite/usage_store.go`.
- **Atividade reutiliza** `stats_store.go` (Activity, StatsTotals, ProjectsPerBucket...). Essas consultas passam a aceitar filtro de **projeto** e intervalo livre; `StatsService` (Dashboard) e `AnalyticsService` chamam as mesmas funções. A janela de período, os intervalos e a regra do "mesmo tempo decorrido" no período anterior saem do `StatsService` para um helper compartilhado.
- Filtro comum: `AnalyticsQuery{Period, From, To (datas locais, para "custom"), Providers, Project, Model}`. **Modelo** só é exposto na tela Usage (a atividade não tem modelo por mensagem); Overview e Activity não o recebem. *(Decisão do implementador.)*
- Períodos: hoje, 7, 30, 90 dias, este mês, mês anterior, personalizado e **Tudo** (como no Dashboard; registros de consumo sem horário só entram em "Tudo"). *(Decisão do implementador.)* Limites em **meia-noite local** do sistema operacional: `[início do primeiro dia, início do dia seguinte ao último)`; "hoje" e os períodos que terminam hoje vão até agora. Intervalo: dia até 92 dias, semana (segunda) até 2 anos, mês acima, como no Dashboard.
- Período anterior para comparação: mesma duração imediatamente antes (mesma regra do Dashboard). Mês anterior compara com o mês antes dele.
- Retornos tipados com cobertura por métrica (§3.5). Nenhuma regra de negócio no React.
- Tudo em SQL agregado; rankings com limite (top N) e tabela de sessões paginada.

### Métricas
- **Overview**: total de tokens; entrada, saída, cache lido, cache criado; por provider; série temporal (empilhada por provider); top projetos por tokens; comparação com o período anterior.
- **Usage**: por provider, por modelo, por projeto, por sessão (paginado; inclui duração = última − primeira mensagem com horário), cache (lido/criado e proporção sobre a entrada total), reasoning (onde disponível), por origem (principal × subagente).
- **Activity**: sessões por intervalo, por provider, por projeto; mapa dia da semana × hora (mensagens, horário local); frequência de uso (dias com atividade no período, sessões por dia ativo); comparação com o período anterior. Unidades das mesmas regras do Dashboard (mensagem = `CountsAsMessage`).

## 7. Frontend
- Item **Analytics** na barra lateral; rotas `/analytics` (Overview), `/analytics/usage`, `/analytics/activity`; abas no topo.
- Barra de filtros compartilhada, com estado na URL (voltar e trocar de aba mantêm o filtro): período (com intervalo personalizado por dois campos de data), pílulas de provider (mesma regra das listas), projeto.
- Gráficos SVG próprios (`components/charts`), com eixos, unidades ("tokens", "mensagens", "sessões") e legenda. Novos: barras empilhadas horizontais (composição entrada/saída/cache) e mapa de calor 7×24.
- Todo card: carregando, vazio, erro. Campo indisponível mostra "indisponível" com dica do motivo ("o Claude Code não informa reasoning"); parcial mostra selo com a cobertura; derivado mostra marca "derivado".
- Números grandes formatados (`1,2 mi tokens`) com valor exato na dica.
- Overview orientado a consumo; **não** repetir os indicadores de atividade do Dashboard ali.

## 8. Testes (obrigatórios)
Fixtures reais **anonimizadas** (estrutura e números preservados; texto, caminhos e IDs trocados) em `docs/discovery/fixtures`:
1. Claude: resposta em 3 linhas → 1 registro; total derivado; reasoning indisponível.
2. Claude: mesma resposta no principal e no subagente → conta 1 vez (origem principal); `toolUseResult.totalTokens` ignorado.
3. Codex: evento repetido ignorado; soma dos pedidos = acumulado final; entrada sem cache derivada; acumulado não somado.
4. Codex: acumulado que diminui (retomada).
5. OpenCode: total informado; total ausente com reasoning 0 (derivado) e com reasoning > 0 (indisponível); `step-finish` ignorado.
6. Regra consumo × atividade, com fixture de uma sessão principal, dois subagentes, uma automática e uma subordinada sem pai no índice: (a) consumo total inclui todas as origens; (b) a principal consolida os descendentes associados; (c) o detalhamento por origem reconcilia com o total; (d) a não associada continua contabilizada e marcada; (e) atividade exclui subagentes e automáticas; (f) atividade = Dashboard para os mesmos filtros.
7. Sessão sem consumo → `unavailable`; campo ausente não vira 0 nas somas; cobertura correta.
8. Releitura idempotente (indexar duas vezes = mesmas linhas); recuperação (falha no meio da fonte → rollback → releitura completa).
9. Períodos e fuso: virada de dia local, "este mês", "mês anterior", personalizado.
10. Consistência: sessões e mensagens do Activity = Dashboard para o mesmo filtro.
11. Agrupamentos por provider, modelo, projeto.

Ao final, uma tabela de valores esperados × calculados para cada caso.

## 9. Documentação
`docs/ANALYTICS.md` (novo: métricas, regras por provider, **a regra de domínio consumo × atividade**, limitações), `docs/DOMAIN.md` (Usage), `docs/ARCHITECTURE.md`, `docs/ROADMAP.md`, `docs/PROVIDERS.md` (campos de consumo), `CHANGELOG.md`.

## 10. Deixado ao implementador
- Nomes internos, formato exato de `derived`, layout fino das telas dentro do design system Aurora.
- Escolha de N nos rankings (padrão 10) e tamanho de página da tabela de sessões (padrão 50).
