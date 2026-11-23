# Analytics

Seção **Analytics** (barra lateral): consumo de tokens, custo estimado e uso dos agentes, calculados a partir dos históricos locais. Nada sai da máquina. Cinco telas compartilham os filtros de período, agente e projeto (na URL):

- **Visão geral**: total de tokens, entrada, saída, cache lido e criado (com variação contra o período anterior), tokens por intervalo e agente, distribuição por agente, composição e projetos com maior consumo.
- **Consumo**: os mesmos campos por agente, modelo, projeto e origem; cache; reasoning; tabela de sessões paginada e expansível.
- **Atividade**: sessões e mensagens com variação, sessões por intervalo (anterior tracejado), por agente e por projeto, mapa dia da semana × hora e frequência de uso (dias com atividade, sessões por dia ativo).
- **Custos** (Fase 9): custo **estimado** com os preços que o usuário cadastra; por agente, modelo, projeto, sessão, intervalo e categoria de token; qualidade da estimativa; sessões sem custo calculável; Recalcular; Exportar CSV. A tabela de preços fica em **Custos → Preços**.
- **Padrões** (Fase 9): métricas de atividade observada (calendário, iniciadas e retomadas, sequências, distribuições, projetos, agentes); Exportar CSV.

Consumo e Custos têm o filtro de **modelo**.

Nenhuma métrica é rotulada como produtividade, eficiência ou qualidade. Nenhum custo é apresentado como valor cobrado.

## Regra de domínio: consumo × atividade

> Consumo de tokens inclui subagentes e sessões automáticas. Atividade considera somente sessões principais e não automáticas.

Esse texto aparece em todas as telas do Analytics (exceto a tabela de preços). A regra é de domínio, não de apresentação, e tem testes (`internal/service/analytics_test.go`, `TestDomainRule*` e `TestActivityMatchesDashboard`).

- **Consumo** é a utilização técnica real e **inclui tudo**: sessões principais, subagentes e automáticas. A visibilidade no Session Manager nunca exclui consumo.
- **Atividade** segue **exatamente** o Dashboard: as mesmas consultas (`stats_store.go`) e a mesma regra de visibilidade (`parent_native_id IS NULL AND automated = 0`). Não há controle para incluir subagentes ou automáticas na Atividade. Para os mesmos filtros, Dashboard e Atividade mostram os mesmos números.
- **Origem** de uma sessão no consumo:
  - `subagent`: tem sessão pai, mesmo que também seja automática;
  - `automatic`: automática e sem pai;
  - `main_agent`: as demais.
- **Associação**: uma sessão subordinada é *associada* quando a sessão pai está no índice. Sem pai no índice, ela continua no consumo e aparece como **não associada**.
- **Tabela por sessão**: cada linha é uma sessão raiz (principal, automática sem pai, ou subordinada não associada) com o consumo dos descendentes associados somado. Ao expandir, o detalhamento por origem fecha com a linha.

## Modelo de dados

Um registro por **resposta** (Claude Code, OpenCode) ou **pedido** (Codex), na tabela `usage` do `index.db` (migration `0004_usage`, que força a releitura de todos os históricos). Campos:

- `response_id`, `ts` (ms UTC) e `model`;
- `input_tokens`, `output_tokens`, `cache_read_tokens`, `cache_creation_tokens`, `reasoning_tokens` e `total_tokens`;
- `derived`: campos calculados, por exemplo `total`;
- `status`: `complete` ou `partial`.

Projeto, provider e origem vêm da sessão.

- **`NULL` = indisponível, nunca zero.** Os agregados devolvem soma e cobertura (`Field{sum, count}`): `count < records` é **parcial**; `count = 0` é **indisponível**. A interface mostra "parcial · N%" ou "indisponível" com o motivo, nunca `0`.
- **Entrada não inclui cache.** O Codex conta o cache na entrada, por isso lá a entrada é derivada (`input − cached`).
- **Reasoning nunca entra no total.** Aparece como categoria à parte.
- `status = partial` quando falta algum campo que o provider costuma informar. Um campo que o provider nunca informa (ex.: reasoning do Claude) não torna o registro parcial.
- **Por que por resposta, e não por sessão ou por linha:** agregar por sessão perde a série temporal e a troca de modelo no meio da sessão; gravar por linha repetiria o Claude, que grava cada resposta em várias linhas.

### Deduplicação

1. **No adaptador**, dentro da fonte: Claude por `message.id` (depois `requestId`, depois a linha); Codex por acumulado da sessão; OpenCode por mensagem.
2. **No índice**, `UNIQUE (session_id, response_id)`.
3. **Na consulta**, entre sessões: a mesma resposta em duas sessões do mesmo provider (ex.: a cópia de contexto que o Claude grava no arquivo do subagente) conta uma vez. Vale a cópia com horário (para a resposta não sumir dos períodos com data); depois a da sessão principal; entre iguais, a mais antiga, depois a de menor sessão. `response_id` vazio nunca é tratado como a mesma resposta. Feito na consulta, não depende da ordem de indexação e sobrevive à releitura de um arquivo só.

### Valores inválidos

- **Negativo** não é consumo: o campo fica indisponível (registro `partial`) e não entra em total nem em entrada derivados. No Codex, `cached_input_tokens` maior que `input_tokens` deixa a entrada indisponível.
- **Tipo inesperado** (decimal, texto, número acima de int64): o consumo daquela resposta é descartado (conta em itens ignorados no log), e a mensagem continua no viewer e na busca.
- **Claude sem `message.id`, `requestId` e `uuid`:** a linha vira a identidade (`line:N`), nunca junta respostas diferentes.

## O que cada agente informa

| Campo | Claude Code | Codex | OpenCode |
|---|---|---|---|
| unidade | resposta (`message.id`) | pedido (`event_msg/token_count`) | mensagem `assistant` |
| entrada | `input_tokens` (sem cache) | **derivada**: `input_tokens − cached_input_tokens` | `tokens.input` |
| saída | `output_tokens` | `output_tokens` (inclui reasoning) | `tokens.output` |
| cache lido | `cache_read_input_tokens` | `cached_input_tokens` | `tokens.cache.read` |
| cache criado | `cache_creation_input_tokens` | **indisponível** | `tokens.cache.write` |
| reasoning | **indisponível** (vem dentro da saída) | `reasoning_output_tokens` (dentro da saída) | `tokens.reasoning` |
| total | **derivado**: entrada + saída + cache lido + cache criado | `total_tokens` (= `input_tokens` + `output_tokens` brutos, ou seja entrada + cache lido + saída) | `tokens.total`; se ausente, derivado só com reasoning = 0 |
| modelo | `message.model` | `turn_context.model` vigente | `modelID` da mensagem |

### Claude Code
- Uma resposta ocupa várias linhas, uma por bloco, todas com o mesmo `usage` (1.912 de 2.803 respostas numa sessão real; nenhuma divergência observada). As linhas nem sempre são contíguas. Conta uma vez, com a última linha. Se os valores divergirem, a divergência vai para o log, sem conteúdo.
- `toolUseResult.totalTokens/usage` da ferramenta Task é o resumo do subagente, que já tem o próprio arquivo: **não conta**.
- Mensagens `<synthetic>` são geradas pelo próprio Claude Code, sem chamada à API: sem consumo.
- `input_tokens` não inclui cache: confirmado nos dados (mediana de 2 tokens contra 240 mil de cache lido).

### Codex
- Cada `token_count` traz `last_token_usage` (o pedido) e `total_token_usage` (acumulado da sessão). Só o pedido é somado; o acumulado nunca.
- Os eventos não têm ID. A identidade é `<sessão>#<acumulado>`: evento cujo acumulado não avança é repetição. Acumulado que diminui (sessão retomada) abre um novo trecho (`@N`).
- **Reconciliação**: a soma dos pedidos de cada trecho deve igualar o acumulado. Se não igualar, vai um aviso para o log (`soma dos pedidos difere do acumulado da sessão`), sem conteúdo.
- Sessões antigas sem `token_count` com `info` não têm consumo (indisponível).

### OpenCode
- Um registro por mensagem `assistant` com `tokens`. As partes `step-finish` repetem as contas por passo e não contam.
- `total` informado = entrada + saída + cache em 145 de 145 registros locais. Se reasoning está dentro da saída **não dá para verificar localmente** (todos os registros têm reasoning 0). Por isso, sem total informado, o total só é derivado quando reasoning é 0; com reasoning > 0 ou ausente, o total fica indisponível.

## Períodos e fuso

Fuso local do sistema operacional, com limites em meia-noite local. Os períodos são:

| Período | Janela | Intervalo do gráfico |
|---|---|---|
| Hoje | até amanhã à meia-noite; a série vai só até a hora atual | **hora** |
| 7, 30 e 90 dias | até amanhã à meia-noite, como no Dashboard | dia |
| Este mês | do dia 1 até amanhã à meia-noite | dia |
| Mês anterior | o mês inteiro | dia |
| Personalizado | do dia inicial até o fim do dia final (inclusive) | dia até 92 dias; semana (começa na segunda) até 2 anos; mês acima disso |
| Tudo | desde o primeiro registro | semana até 2 anos de histórico, mês acima disso |

**Comparação** com o período anterior:
- **Totais:** usam o mesmo tempo já decorrido, para não comparar um período em andamento com um inteiro.
- **Este mês:** compara com o mesmo número de dias do mês anterior.
- **Mês anterior:** compara com o mês antes dele, inteiro.
- **Série tracejada:** usa os intervalos inteiros do período anterior e só aparece quando o número de intervalos é igual ao do atual. Em "Hoje", ontem é cortado na mesma hora.
- **Tudo:** não tem comparação.

Registros de consumo **sem horário** só entram em "Tudo".

## Desempenho

Agregação em SQL; a tabela de sessões tem 50 linhas por página. A deduplicação entre sessões só compara as respostas repetidas (`dups`), usando o índice `usage_resp_idx (response_id, session_id, ts)`.

Visão geral e Consumo fazem **uma** consulta agrupada (`UsageBreakdown`: provider × modelo × projeto × origem), e os agrupamentos são somados em Go.

Medido numa cópia do índice real (32,5 mil registros, cache quente, melhor de 4), depois das correções da revisão de 2026-11-23:

| Tela | 30 dias | 90 dias | Tudo |
|---|---|---|---|
| Visão geral | 115 ms | 139 ms | 114 ms |
| Consumo | 67 ms | 88 ms | 77 ms |
| Sessões | 106 ms | 119 ms | 108 ms |
| Atividade | 90 ms | 109 ms | 114 ms |

Meta: 150 ms. A Visão geral em 90 dias é a mais perto do limite.

Fase 9, mesma cópia do índice (32,9 mil registros, 78 preços):

| Operação | 30 dias | 90 dias | Tudo |
|---|---|---|---|
| Custos | 128 ms | 143 ms | 126 ms |
| Custo por sessão | 81 ms | 112 ms | 107 ms |
| Sem custo calculável | 55 ms | 78 ms | 67 ms |
| Padrões | 134 ms | 109 ms | 106 ms |

`Refresh` das estimativas: 622 ms na primeira vez (32.939 estimativas, uma vez só, em segundo plano) e 197 ms quando nada mudou (roda depois de cada sincronização; meta 300 ms). Prévia do Recalcular em "Tudo": 623 ms (ação explícita).

## Custos (Fase 9)

**Todo custo é uma estimativa**: o valor que os tokens custariam com os preços que o usuário cadastrou. Não é o valor cobrado. Planos por assinatura não são cobrados por token, e o app não sabe o que o usuário paga. O selo "Estimativa" e esse aviso aparecem fixos na tela.

### Preços
- Cadastrados pelo usuário em **Custos → Preços**. O app **não consulta preços** e a tabela começa vazia.
- **Cada preço** vale para um agente e um modelo: id exato ou prefixo terminado em `*` (`claude-opus-*`). Tem também:
  - moeda (ISO 4217, 3 letras);
  - unidade de cobrança (por token, por mil ou por milhão de tokens);
  - preço de entrada, saída, cache lido e cache criado;
  - vigência (do dia inicial ao final, inclusivo; sem fim é permitido);
  - origem e observação.
- **Categoria vazia** = sem preço (não é zero; a estimativa fica parcial se houver tokens nela). `0` = gratuito. Reasoning não tem preço: vem dentro da saída, ou não se sabe se vem.
- **Decimal:** até 6 casas, ponto ou vírgula na tela; máximo 1.000.000 por unidade.
- **Moeda padrão** (`config.json`, `defaultCurrency`, padrão BRL): só preenche o formulário. Editada à mão, é normalizada para maiúsculas; um valor inválido volta para BRL sem descartar o resto da configuração.
- **Revisões.** Um preço nunca é alterado no lugar:
  - **Editar** corrige o preço: grava uma revisão nova (`replaced_by` na anterior). Estimativas já calculadas continuam com a revisão antiga.
  - **Novo preço a partir de…** registra uma mudança no tempo. Encerra a vigência do preço atual no dia anterior e cria um preço novo a partir da data. Fechar a vigência é a única alteração no lugar (`valid_to` + `closed_at`), e os valores não mudam. Respostas a partir dessa data que já tinham estimativa continuam com o preço anterior até o Recalcular; ao salvar, a tela diz quantas são.
  - **Encerrar vigência:** fecha o preço no último dia escolhido **sem cadastrar sucessor** e sem criar revisão (`valid_to` + `closed_at`, só se ainda sem fim). Depois da data, sem outro preço vigente, as respostas ficam **sem preço** — o preço encerrado nunca vale indefinidamente. Um sucessor pode ser cadastrado depois, a partir do dia seguinte (sobreposição é recusada). Como no "novo preço a partir de", estimativas já calculadas depois da data continuam com o preço até o Recalcular, e a tela diz quantas são.
  - **Desativar:** não vale para cálculos novos; estimativas antigas mantêm a revisão.
  - **Reativar:** cria uma revisão nova.
  - **Histórico:** todas as revisões, com data, valores, vigência e quantas estimativas usam cada uma.
- **Validação:**
  - moeda;
  - unidade;
  - decimais;
  - pelo menos uma categoria com preço;
  - fim depois do início;
  - sem sobreposição de vigência entre preços ativos do mesmo agente e modelo (sem diferenciar maiúsculas).

### Qual preço vale
Para cada resposta:
- **agente:** o da sessão;
- **data:** o horário da resposta dentro de `[início, fim)`;
- **modelo:** o id exato vence; senão, o prefixo `*` mais longo;
- **revisão:** só revisões ativas (não substituídas nem desativadas).

Resposta **sem horário** não tem estimativa (sem data não há vigência).

### Fórmula
```
custo_categoria = tokens_categoria × preço_categoria / unidade
custo_total     = soma das categorias calculadas
```
- **Categorias:** entrada (sem cache), saída, cache lido e cache criado. A entrada já vem sem cache em todos os agentes (no Codex é derivada).
- **Aritmética:** dinheiro em **nanos** (10⁻⁹ da moeda), `int64`. A conta é feita em `math/big`: `tokens × preço_em_micros × 1000 / unidade`, arredondada *half-even* no nano. Somas em SQL são inteiras e exatas. **Nunca `float64`.**
- **Exibição:** arredonda *half-even* na própria string decimal, com 2 casas. Abaixo de 0,01 (e diferente de zero) usa até 6 casas; abaixo de 0,000001 mostra "< 0,000001". O CSV leva as 9 casas, sem arredondar.

### Status de cada estimativa
| status | quando |
|---|---|
| completa (`complete`) | todas as categorias que o agente informa e que têm tokens foram calculadas |
| parcial (`partial`) | algum custo calculado, mas falta parte: categoria com tokens e sem preço, ou categoria que o agente costuma informar e veio indisponível |
| sem preço (`no_price`) | há tokens, mas nenhum preço ativo casa com agente, modelo e data, ou nenhuma categoria com tokens tem preço (nunca vira R$ 0,00) |
| sem dados (`insufficient`) | sem horário, sem modelo, sem nenhum token ou valor acima do limite |

- Categoria que o agente **nunca** informa (cache criado do Codex) não torna a estimativa parcial (mesma regra do consumo).
- Categoria com 0 tokens custa 0, mesmo sem preço.
- Os motivos (`reasons`) são textos fixos (ex.: "cache criado sem preço"), nunca conteúdo de conversa.

### Estimativas gravadas e recálculo
Uma estimativa por registro de consumo, no `user.db` (`cost_estimates`), pela chave estável `(agente, sessão nativa, response_id)`. Isso inclui as cópias que a deduplicação descarta. Cada estimativa guarda:
- a revisão do preço;
- a moeda e a unidade;
- os tokens usados;
- o custo por categoria e o total;
- o status e os motivos;
- o horário da resposta;
- a data do cálculo.

**Quando muda:**
- **Depois de cada sincronização** (`Refresh`):
  - registro sem estimativa → preço ativo no horário da resposta;
  - estimativa sem preço ou sem dados → recalculada, porque não há preço a preservar. Cadastrar o preço que faltava já mostra o custo;
  - tokens diferentes dos gravados (resposta crescendo numa sessão ativa) → recalculada **com a mesma revisão**, mesmo desativada — desde que a vigência dela ainda cubra o horário da resposta; com a vigência encerrada antes, vale o preço vigente naquele horário (ou nenhum);
  - completas e parciais com os mesmos tokens **nunca mudam sozinhas**.
- **Depois de cadastrar, editar, encerrar, desativar ou reativar um preço:** o mesmo `Refresh`, em segundo plano. O evento `costs:updated` recarrega a tela.
- **Recalcular (explícito):**
  - aplica os preços ativos a **todas** as respostas do recorte da tela, inclusive as cópias descartadas pela deduplicação (assim, se a cópia que conta mudar, o preço novo continua valendo);
  - antes de gravar, mostra quantas estimativas mudam e o total por moeda antes → depois (deduplicado);
  - **a prévia não grava nada** (nem as pendências do `Refresh`, calculadas em memória) e **tem um token**: SHA-256 do plano do recorte — cada estimativa a gravar (a gravada e a nova, sem a data do cálculo), as respostas que contam nos totais (deduplicadas) e os totais antes → depois mostrados. Ao confirmar, o plano é refeito sob o mutex e só é gravado se o token for o mesmo. Se uma sincronização, um preço ou outro recálculo mudou algo do recorte entre a prévia e a confirmação, nada é gravado e a tela mostra a prévia nova ("os dados mudaram desde a prévia"). Mudança fora do recorte não invalida a prévia (as pendências do `Refresh` fora dele são gravadas junto, recalculadas na confirmação). Nenhuma transação fica aberta enquanto o usuário decide;
  - grava numa transação: falha no meio do lote desfaz tudo (a mesma prévia continua válida).

`Refresh`, `Recalculate` e o cadastro de preços passam pelo mesmo mutex e nunca rodam ao mesmo tempo. Além disso, editar, encerrar e "novo preço a partir de" usam `UPDATE` condicional no banco (`replaced_by IS NULL`, `valid_to IS NULL`): duas mudanças no mesmo preço nunca passam as duas, mesmo fora do mutex.

**Concorrência testada** (`cost_concurrency_test.go`, `recalc_consistency_test.go`, com `-race`): os cenários usam canais e um gancho de teste que pausa o Recalcular com o mutex preso (sem `sleep`) e conferem o banco no fim — uma revisão atual por preço, nenhuma vigência sobreposta, toda estimativa aponta para revisão existente e tem os tokens atuais do índice. Cobertos: duas edições do mesmo preço; encerrar × encerrar × novo preço a partir de; o mesmo encerramento direto no banco; leitura, sincronização e `Refresh` durante a gravação do Recalcular; falha na gravação com outra operação concorrente; duas confirmações da mesma prévia (uma grava, a outra é recusada). Isso não prova ausência de corrida em geral: vale para esses cenários e para o que o `-race` observa nas execuções. Se a sessão some do índice, a estimativa fica no `user.db`, fora dos relatórios; se a sessão voltar, volta com o preço preservado.

### Relatórios
- Os relatórios usam o consumo **deduplicado** da Fase 8 (mesma CTE) junto das estimativas. Uma resposta copiada no subagente custa uma vez.
- **Moedas nunca se somam nem se convertem:** todo total e agrupamento sai por moeda.
- **Ordenação dos rankings** (por agente, modelo, projeto e sessão): moedas nunca se comparam. Cada linha entra na seção da **primeira moeda com custo em ordem alfabética do código** (BRL, EUR, USD…); dentro da seção, pelo total daquela moeda (maior primeiro), depois pelo número de respostas e pela chave; linhas sem custo calculado vêm no fim. Com mais de uma moeda, a tela mostra o cabeçalho de cada seção ("Em BRL", "Em USD", "Sem custo calculado"). O limite de itens (top N) vale em cada moeda: um grupo aparece se estiver entre os N maiores de **qualquer** moeda que tenha (um modelo com pouco BRL e muito USD não some). Uma linha com BRL e USD aparece na seção BRL e mostra os dois valores, mas só o BRL decide a posição.
- **Composição por categoria:** a % da legenda é calculada em inteiro sobre os nanos (sem float), arredondada pelo maior resto para somar 100%; parte > 0 que arredondaria para 0 mostra "<1%" e parte que não é o total inteiro nunca mostra 100% (">99%"). O número só desenha a largura da barra.
- Registros sem custo calculado aparecem contados (qualidade, "sem custo"), nunca como R$ 0,00.
- **Telas:**
  - total por moeda com variação (mesmo tempo decorrido do período anterior) e cobertura ("estimativa sobre N% das respostas");
  - qualidade (completas, parciais, sem preço, sem dados);
  - evolução por intervalo (automático, dia, semana ou mês);
  - por agente, modelo e projeto;
  - composição por categoria;
  - por sessão raiz (com subagentes associados), com link para a conversa;
  - sessões sem custo calculável, com motivo.

## Linha do tempo da sessão

Na tabela **Consumo → Por sessão**, ao expandir uma sessão raiz: além das origens, o consumo **por intervalo** ao longo da sessão (útil sobretudo em sessões retomadas).

- **Fonte:** os registros de consumo já indexados, com a mesma CTE dos relatórios: deduplicação entre sessões (a resposta copiada no subagente conta uma vez), descendentes associados somados à raiz, subordinada sem pai na própria linha (marcada "não associada"), mesmos filtros e período da tela. Nada é estimado a partir do texto.
- **Intervalo:** o menor de 1, 5, 15 ou 30 min, 1, 3, 6 ou 12 h ou 1 dia que cobre a sessão em menos de 60 intervalos, alinhado ao dia local. **Só há barra onde há registro**: intervalo sem consumo não vira ponto zero.
- **Barras:** entrada, saída, cache lido e cache criado empilhados, só os componentes que algum registro informou. **Reasoning fica fora da pilha** (no Codex vem dentro da saída; no OpenCode não se sabe): aparece só no detalhe do intervalo. O detalhe (passar o mouse) mostra cada componente, registros, acumulado e se há dados parciais.
- **Indicadores:** intervalo com algum componente ou total ausente em parte dos registros = parcial (barra mais clara e aviso). Registros sem horário entram no total, fora do gráfico, com aviso. **Retomada** = registro depois de pausa ≥ 30 min desde o anterior da mesma raiz (linha tracejada e contagem).
- **Reconciliação:** soma dos intervalos + registros sem horário = linha da sessão na tabela, campo a campo (soma e quantos informaram). Testado em todas as sessões dos fixtures, com um evento, vários, subagentes, horários iguais e cópias deduplicadas.

## Padrões (Fase 9)

Fixo na tela: **"Métricas de atividade observada nos históricos. Não medem produtividade, qualidade, esforço ou valor entregue."** Não há tempo ativo, pontuação, ranking de eficiência nem cruzamento de tokens ou custo com produtividade.

Mesma regra da Atividade e do Dashboard: só sessões principais e não automáticas; "mensagem" = usuário ou assistente com texto; fuso local.

| Indicador | Definição |
|---|---|
| Sessões iniciadas | sessões com início (`created_at`) no período, por intervalo; comparação pelo mesmo tempo decorrido, série anterior tracejada |
| Sessões retomadas | sessões com uma mensagem no período que veio depois de uma pausa ≥ **30 min** desde a mensagem anterior **da mesma sessão** (a anterior pode estar antes do período; a sessão pode ter começado no período). Só mensagens com texto medem a pausa. Cada sessão conta uma vez |
| Dias e semanas com atividade | dias locais com mensagem; semanas (segunda a domingo) com algum dia ativo. Dias com atividade = o da tela Atividade |
| Sessões por dia ativo | sessões com mensagem no período ÷ dias ativos |
| Dias seguidos | sequência atual e maior sequência, no período, por **data civil local**. A atual termina na data de referência; se ela é **hoje** e ainda não houve atividade, vale a sequência que terminou ontem (o dia não acabou: seg, ter e qua com atividade, quinta às 8h sem nada → 3). Só um dia inteiro sem atividade interrompe. Anda de dia em dia pela data (não por 24 h), então dias de 23/25 h e meia-noite inexistente no horário de verão não pulam nem repetem dia. O dia de cada mensagem (aqui, no calendário e em dias ativos) usa o deslocamento do fuso **naquele instante** (`time.ZoneBounds`), não o de agora |
| Calendário | uma célula por dia (sessões e mensagens), até o último ano do período |
| Projetos mais ativos | por sessões, com mensagens, dias ativos e dias desde a última mensagem; top 10 |
| Projetos recorrentes | projetos com atividade em 2 dias ou mais no período |
| Dias e horários | o mapa da tela Atividade |
| Duração observada | da primeira à última mensagem com horário da sessão (todo o histórico dela); mediana, "90% até" e faixas (< 5 min, 5–30 min, 30 min–2 h, > 2 h). **Não é tempo de trabalho** |
| Mensagens por sessão | mensagens no período; faixas 1–5, 6–20, 21–50, > 50 |
| Intervalo entre sessões | entre inícios de sessões consecutivas no recorte; faixas < 1 h, 1–24 h, 1–7 dias, > 7 dias |
| Vários agentes no mesmo projeto | projetos com sessões de 2 agentes ou mais, com as sessões de cada um |
| Comparação entre agentes | sessões, mensagens, dias ativos e duração mediana por agente |

- **Mediana e "90% até":** *nearest-rank* (o valor na posição ⌈p·n⌉).
- **Data de referência** de "dias seguidos" e "dias desde a última": o fim do período, limitado a hoje. Em "Mês anterior", é o último dia daquele mês.

## Exportação CSV

**Exportar CSV** em Custos e em Padrões: o backend monta o arquivo do recorte atual e grava onde o usuário escolher (diálogo nativo). Nada sai da máquina; o arquivo é gravado com permissão `0600`.

- **Formato longo**, UTF-8, vírgula, ponto decimal, uma linha por grupo e métrica:
  `periodo_inicio, periodo_fim, agente, modelo, projeto, sessao, metrica, valor, moeda, status, cobertura`.
- **Custos:**
  - por agente × modelo × projeto × moeda: `custo_total`, `custo_entrada`, `custo_saida`, `custo_cache_lido`, `custo_cache_criado` (9 casas), `respostas`, `respostas_completas`, `respostas_parciais`, `respostas_sem_preco`, `respostas_sem_dados`;
  - por sessão raiz: `custo_total`;
  - `status` é o mais fraco do grupo e `cobertura` é a % de respostas com custo, os dois calculados sobre o grupo inteiro (todas as moedas e as respostas sem custo).
- **Padrões:** indicadores gerais (`sessoes_iniciadas`, `sessoes_retomadas`, `dias_ativos`, `duracao_mediana_ms`…), por projeto, por agente e por projeto × agente. Moeda, status e cobertura ficam vazios.
- **Consistência:** grupos e sessões de custo são lidos sob o mesmo mutex do Recalcular (o arquivo nunca mistura estado antigo e novo).
- **Datas:** ISO 8601 com o fuso local. A sessão aparece como `agente:id`, **nunca o título**, e nenhum texto de conversa entra.
- **BOM UTF-8** (`EF BB BF`) no início, uma vez, antes do cabeçalho: sem ele o Excel no Windows abre o arquivo como Windows-1252 e troca os acentos. O resto do arquivo é UTF-8 sem mudança. LibreOffice, Google Sheets, pandas e o `encoding/csv` do Go aceitam o BOM; um leitor que não o trate vê `\uFEFF` antes de `periodo_inicio`.
- **Proteção contra fórmula (CSV injection):** toda célula de **texto** (agente, modelo, projeto, sessão, moeda) que começa com tabulação ou CR, ou cujo primeiro caractere depois de espaços, caracteres de controle e invisíveis de formatação (U+200B, U+FEFF…) é `=`, `+`, `-` ou `@`, recebe `'` na frente (recomendação OWASP). Números (`valor`, `cobertura`) nunca passam pela proteção, então valor negativo continua número. Aspas, vírgulas e quebras de linha são escapadas pelo `encoding/csv` (sem montar linha por concatenação). O dado no banco não muda. Limites: o Excel mostra o `'` como parte do texto em alguns casos (ex.: depois de editar a célula); outro leitor de CSV vê o apóstrofo literalmente; um nome de projeto que comece mesmo com `-` sai com o apóstrofo.

## Limitações

- **Codex:**
  - o vínculo com a sessão pai (`parent_thread_id`) não é lido, então subagentes do Codex aparecem como principal ou automática;
  - não informa cache criado;
  - se uma sessão retomada (ou bifurcada) copiar os eventos `token_count` antigos para um arquivo novo, os pedidos copiados seriam somados de novo: a identidade inclui a sessão. Não verificável localmente (há uma sessão Codex só);
  - um evento antigo repetido depois de outros pode ser lido como retomada (acumulado menor) e somado de novo.
- **Claude Code:** não informa reasoning separado.
- **OpenCode:** não dá para confirmar localmente se reasoning está dentro da saída (ver acima).
- **Horário de verão:** o deslocamento do fuso é o atual e vale para todo o período. Onde há horário de verão, mensagens perto da meia-noite podem cair no dia vizinho (no Brasil não há efeito). Mesma limitação do Dashboard.
- **Duração por sessão:** é a da sessão raiz (primeira à última mensagem com horário); subagentes não estendem.
- **Profundidade:** a subida até a sessão raiz para em 16 níveis. Uma cadeia mais funda (ou um ciclo nos dados) não trava a consulta: a sessão fica como raiz própria, não associada.
- **Clique no gráfico de sessões:** abre a lista de sessões sem filtro de data.
- **Custos:**
  - um preço só para o cache criado. O Claude cobra a gravação de 5 min e a de 1 h de forma diferente, e o histórico não separa;
  - descontos (lote, faixas de volume, contexto longo com outro preço) não são modelados;
  - assinatura não vira custo: a estimativa não representa o que o usuário paga;
  - resposta sem horário fica sem estimativa;
  - a comparação depois de cada sync relê todas as estimativas (O(n), 197 ms com 32,9 mil). Se crescer, fazer por sessão alterada.
- **Padrões:**
  - a pausa da retomada (30 min) é fixa;
  - a duração observada inclui pausas (a sessão retomada no dia seguinte "dura" um dia).

## Dados de exemplo

`packaging/scripts/demo-home.py DIR` gera um `HOME` com históricos sintéticos dos três agentes, com consumo, para capturas de tela. Os testes usam fixtures próprios em `docs/discovery/fixtures/usage/`: estrutura real, números reais e todo o resto fictício. Os valores esperados estão no README desses fixtures, e `TestPrintVerificationTable` imprime a comparação.
