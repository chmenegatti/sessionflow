# Redesign visual "Aurora" e Dashboard analítico

**Data:** 2026-11-22 · **Status:** aguardando aprovação · **Referência visual:** `docs/superpowers/specs/2026-11-22-redesign-aurora/tema-b.png` (maquete aprovada) e `serf dashboard` (21st.dev, @anark17r) como inspiração de estrutura.

## Intenção

O usuário acha o visual atual simples e sem graça e quer modernidade e beleza **em todos os pontos do app**, com sombra, transparência e efeitos. Entre quatro paletas mostradas (Manuscrito, Aurora, Terminal, Papel), escolheu a **B · Aurora**. Ele também escolheu que o Dashboard ganhe os painéis analíticos da maquete, que precisam de dados novos, em vez de receber só a "roupa nova".

## Objetivos

1. Aplicar a identidade Aurora a todas as telas e componentes:
   - fundo azul-noite com brilhos ciano, violeta e rosa;
   - peças flutuantes de vidro com desfoque;
   - cards translúcidos com borda iluminada;
   - acento ciano → violeta e brilho nas linhas e nos botões principais.
2. Refazer o Dashboard no formato da maquete:
   - frase-resumo com números em negrito;
   - filtros de período e de agente;
   - 4 indicadores com mini-gráfico e variação contra o período anterior;
   - gráfico de atividade por dia, empilhado por agente;
   - rosca por agente, projetos mais ativos, ferramentas mais usadas;
   - tabela de sessões recentes.
3. Backend novo de estatísticas, com testes, para alimentar o Dashboard.

## Não-objetivos

- Tema claro e alternância de tema: o app continua só escuro.
- Mudar fluxos, rotas ou comportamento das telas além do visual, exceto o Dashboard.
- Biblioteca de gráficos.
- Transparência da janela inteira mostrando o desktop por trás.

## Anti-objetivos (falha mesmo que "funcione")

- Rolagem travando em listas longas ou no viewer por causa de desfoque.
- Texto com contraste ruim sobre o fundo com brilho.
- Efeito que pisca a cada atualização do índice (eventos do watcher chegam a cada poucos segundos).
- Gráfico que fica com cara de "gráfico padrão" em vez do brilho da maquete.

## Restrições

- WebKitGTK 6.0 (Wails v3). `backdrop-filter` funciona, mas custa GPU: o desfoque só entra nas peças flutuantes.
- Animações respeitam "reduzir movimento" (regra já existente em `MotionConfig` e no CSS).
- Os dados do Dashboard vêm do índice local (SQLite), sem rede. Nenhum conteúdo de conversa vai para o log.
- Pacotes de release (AppImage, .deb, .rpm) continuam passando em `make test-install`.

## Abordagens consideradas

| | Como | Troca | Decisão |
|---|---|---|---|
| **1. Tokens + superfícies em camadas + SVG próprio** | Paleta e sombras no tema; desfoque só nas peças flutuantes; cards translúcidos sem desfoque; gráficos SVG à mão; serviço de estatísticas no Go | Mais código de gráfico próprio, mas leve e com o brilho exato da maquete | **Escolhida** (recomendação aceita pelo usuário) |
| 2. Igual, com Recharts | Gráficos prontos com interação | +~100 KB e trabalho para chegar ao visual; cara de gráfico padrão | Descartada |
| 3. Vidro em tudo | Desfoque em todo card, linha e bloco | Mais literal, mas pesado no WebKitGTK; listas e viewer travam sem aceleração | Descartada |

## Design

### 1. Sistema visual (tokens, em `frontend/src/index.css`)

Tema escuro único. Os tokens do shadcn (`--background`, `--card`, `--primary`…) passam a valer:

| Token | Valor | Uso |
|---|---|---|
| fundo | `#060914` | base do app |
| superfície | `rgba(255,255,255,.045)` → gradiente `rgba(255,255,255,.075)`→`.025` | cards |
| superfície 2 | `rgba(255,255,255,.07)` | item ativo, controles |
| borda / borda forte | `rgba(255,255,255,.09)` / `.16` | |
| texto / secundário / apagado | `#eef2ff` / `#9aa3c7` / `#636c93` | |
| acento / acento 2 | `#38e1f5` (ciano) / `#a78bfa` (violeta) | primário, foco, seleção |
| positivo / negativo | `#5eead4` / `#fb7185` | variações e estados |
| agentes | Claude `#fb923c`, Codex `#38e1f5`, OpenCode `#c084fc` | pílulas, legendas, gráficos |
| raio | 18 px (cards), 10 px (controles), 999 px (pílulas) | |
| sombra de card | `inset 0 1px 0 rgba(255,255,255,.08), 0 20px 50px -20px rgba(0,0,0,.8)` | |
| brilho | `0 0 24px rgba(56,225,245,.35)` | acento, linhas de gráfico, botão primário |

**Fundo Aurora:** quatro gradientes radiais fixos atrás de todo o app (ciano no canto superior esquerdo, violeta no superior direito, azul no centro e rosa embaixo), como na maquete. É uma camada única do `AppLayout` (`position: fixed`), que não re-renderiza.

**Três superfícies** (classes utilitárias):
- `.glass`: `background: rgba(8,12,30,.45)`, `backdrop-filter: blur(24px) saturate(150%)` e borda. Só para barra lateral, cabeçalho de página, painel de detalhes, diálogos, avisos, tooltips e menus.
- `.surface` (cards): gradiente translúcido, borda iluminada (realce de 1 px no topo via máscara, como na maquete) e sombra. **Sem desfoque.**
- `.surface-sel`: realce de seleção com o gradiente do acento a 16%/8%, borda do acento e brilho.

**Tipografia:** Inter para tudo, com números em `font-variant-numeric: tabular-nums`. JetBrains Mono para IDs, caminhos, atalhos e código. O título da página usa gradiente branco → ciano no texto. As fontes vêm empacotadas pelo npm (`@fontsource-variable/inter` e `@fontsource-variable/jetbrains-mono`), sem rede, como a Geist hoje; a Geist sai.

**Movimento:** os componentes de animação existentes (BlurFade, NumberTicker, Spotlight etc.) continuam. Trocam só as cores dos que têm cor fixa (BorderBeam, TextShimmer, Spotlight) para os tokens novos. Nada de animação nova que dispare em recargas de fundo.

### 2. Shell e componentes compartilhados

Mudar o compartilhado faz todas as telas herdarem o tema.

- **Barra lateral (`.glass`):**
  - logo com o gradiente novo;
  - item ativo com superfície 2, sombra e marcador vertical em gradiente ciano→violeta com brilho;
  - seção "Projetos fixados" com os 3 projetos de maior atividade nos últimos 30 dias e a contagem de sessões de cada um (dado do serviço de estatísticas);
  - rodapé em card com o estado da sincronização ("Sincronizado · há N s", fontes, barra em gradiente). O texto com brilho "Indexando…" continua.
- **Cabeçalho (`PageHeader`, `.glass` fixo):**
  - título ou breadcrumb à esquerda;
  - à direita, no Dashboard, um campo "Buscar em todas as conversas · Ctrl K" que leva à Busca e o botão primário "Retomar última", que retoma a sessão com atividade mais recente.
- **Componentes base:**
  - `Card` vira `.surface`;
  - `Button` primário em gradiente ciano com brilho e realce interno; secundário e ghost translúcidos;
  - `Badge` e pílulas de agente com a cor do agente a 15%, contorno a 30% e texto na cor;
  - `Input`, `NativeSelect` e `textarea` translúcidos, com foco ciano;
  - controle segmentado: pílulas com o item ativo em acento a 16% e contorno;
  - `Tooltip` em `.glass`;
  - `Skeleton` com o brilho em tom azulado;
  - `EmptyState` com o ícone num quadrado de vidro.

### 3. Telas

| Tela | O que muda |
|---|---|
| Dashboard | Refeito (seção 4) |
| Sessões, Favoritas, Projeto (lista) | Barra de filtros com controles segmentados e pílulas de agente; lista num card `.surface` com linhas de hover iluminado (o realce que desliza continua, com a cor nova) |
| Projetos | Cada projeto numa linha do card com as pílulas dos agentes, o número de sessões e a última atividade |
| Busca | Campo grande em vidro com brilho ciano no foco; resultados num card; termos encontrados com fundo ciano a 20% |
| Viewer | Fundo Aurora atenuado; mensagens do usuário em bolha `.surface`; blocos recolhíveis (ferramentas, raciocínio) em superfície 2 com borda; código com tema de cores combinando com a paleta; mensagem-alvo da busca com borda ciano e brilho |
| Configurações | Cards `.surface` por seção; campos translúcidos |
| Painel de detalhes | `.glass`; botões e o comando de retomada em blocos de superfície |
| Diálogo de confirmação, avisos, tela de erro | `.glass` com sombra forte; aviso de erro com acento negativo |

### 4. Dashboard

Layout de cima para baixo, como na maquete `tema-b.png`:

1. **Cabeçalho da página:**
   - linha pequena com a data ("Sábado, 22 de novembro de 2026 · 3 agentes detectados");
   - título "Suas sessões";
   - **frase-resumo** gerada dos dados, por exemplo: "Nos últimos 30 dias você trabalhou em **45 sessões** distribuídas por **31 projetos**, com **3.418 mensagens** trocadas. O **Claude Code** concentrou 78% do trabalho, o pico foi na semana de 10 de novembro e **2 sessões** estão abertas agora."
   - Frases que não se aplicam somem: sem sessões abertas, não fala delas; com um só agente, não fala de concentração.
2. **Filtros:**
   - período segmentado em 7 dias, 30 dias (padrão), 90 dias e Tudo;
   - pílulas por agente (com contagem de sessões no período), que ligam e desligam cada agente;
   - à direita, "Atualiza sozinho" com um ponto de status.
   - Os filtros valem para tudo abaixo.
3. **Quatro indicadores**, cada um com número, variação contra o período anterior de mesmo tamanho (seta e cor; omitida em "Tudo") e mini-gráfico de área por dia:
   - Sessões: com atividade no período;
   - Mensagens: no período;
   - Projetos ativos: com atividade no período, mais o texto "de N no total";
   - Favoritas: total atual, mais "N abertas agora". O mini-gráfico e a variação usam as favoritadas por dia (`favorites.created_at`, no `user.db`); a variação mostra quantas foram marcadas no período ("↑ 2").
   - **Só Sessões e Mensagens trocam a métrica do gráfico grande** (padrão: Mensagens); o selecionado recebe `.surface-sel`. Projetos ativos e Favoritas não são clicáveis.
4. **Atividade por dia:**
   - área empilhada por agente da métrica selecionada, com linha tracejada do período anterior;
   - granularidade: por dia em 7, 30 e 90 dias; em "Tudo", por semana até 2 anos de histórico e por mês acima disso, sem linha do período anterior;
   - linhas com brilho na cor do agente, grade tracejada e eixo de datas;
   - ao passar o mouse, linha vertical e balão de vidro com dia, total e variação contra a média;
   - clicar num dia abre a lista de Sessões filtrada por aquele dia (exige filtro de data na lista; ver "Deixado ao implementador").
5. **Três cards lado a lado:**
   - rosca por agente (sessões no período, total no centro, legenda com número e %);
   - projetos mais ativos (top 5 por mensagens, barras ciano);
   - ferramentas mais usadas (top 5 chamadas, barras violeta).
   - Clicar num projeto abre o projeto.
6. **Agentes** (o card de Providers de hoje, compactado): o card da rosca "Por agente" mostra, em cada linha da legenda, o estado de detecção (ponto verde, amarelo ou vermelho), as sessões ocultas e, ao passar o mouse, o aviso do provider (`providerNote`, erros de leitura). O feixe de "indexando" passa para esse card. O diagnóstico completo (caminhos, CLI, erros) continua em Configurações. Assim o Dashboard segue cumprindo "providers detectados" (CLAUDE.md §7.1).
7. **Sessões recentes:** tabela com sessão (título e ID curto em mono), pílula do agente, projeto (mono), mensagens (barra mais número), atividade relativa, estado (ponto verde com halo "Aberta" se houve escrita no último minuto, senão "Concluída") e estrela de favorita. Segmentado: Todas · Abertas · Favoritas, até 5 linhas cada, todas vindas do `StatsService` e respeitando os filtros de período e de agente. Clicar numa linha seleciona a sessão; duplo clique abre a conversa (o mesmo de hoje).

Os gráficos são componentes SVG próprios (`AreaChart`, `Sparkline`, `Donut`, `BarList`), com gradientes e `drop-shadow` na cor da série, e respeitam "reduzir movimento".

### 5. Backend de estatísticas

- **`StatsService.Dashboard(ctx, StatsQuery{Period: "7d"|"30d"|"90d"|"all", Providers []ProviderID})`** devolve, em uma chamada:
  - totais do período e do período anterior: sessões ativas, mensagens, projetos ativos;
  - total de projetos e de favoritas;
  - sessões abertas: alteradas há menos de 1 min, pela mesma regra da retomada;
  - série diária por agente de sessões ativas e de mensagens no período, com dias sem atividade preenchidos com zero;
  - série diária do período anterior;
  - sessões por agente;
  - top 5 projetos por mensagens;
  - top 5 ferramentas por número de chamadas;
  - dia de pico;
  - as 5 sessões recentes, no formato de item já usado na lista.
- **O que é "mensagem":** a mesma regra de `sessions.message_count`, ou seja, mensagem do usuário ou resposta do agente com texto. Passos só de ferramenta, resultados e metadados não contam. Assim o Dashboard bate com a coluna "Mensagens" das listas. A migration 0003 grava em `messages` um marcador `has_text` (0/1) calculado pelo indexador com a regra de cada provider.
- **Mensagens e dias** vêm de `messages.ts` (ms). O dia é o dia local do usuário, com o fuso do processo. Mensagens sem `ts` não entram nas séries.
- Sessões ocultas (subagentes e automáticas) ficam fora, como no resto do app.
- **Ferramentas:** migration `0003` acrescenta `messages.tools TEXT NOT NULL DEFAULT ''` (nomes das ferramentas chamadas na mensagem, separados por espaço). O sink do indexador preenche a coluna a partir dos blocos `ToolCall`. Os nomes ficam crus, sem tabela de equivalência entre providers: o "Bash" do Claude e o `exec_command` do Codex aparecem como itens separados. Como na 0002, a migration limpa `sync_state` para forçar a releitura (cerca de 20 s em segundo plano na primeira abertura). A contagem usa essa coluna, o que é exato, em vez do texto indexado.
- **"Retomar última":** retoma a sessão principal mais recente que **não** está aberta (sem escrita no último minuto) dentro do filtro de agente do Dashboard, pelo `ResumeService`. Sem candidata, o botão fica desabilitado, com o motivo na dica.
- **Filtros:** período e agente valem para todos os números do Dashboard, inclusive "de N no total", total de favoritas e "N abertas agora".
- **"Projetos fixados" da barra lateral:** vem do mesmo serviço (top 3 projetos nos últimos 30 dias).
- **Desempenho:** índice novo em `messages(ts)` se a consulta pedir. Meta de menos de 150 ms com o índice local (58 mil mensagens). O frontend recarrega com `dataVersion` (watcher), sem esmaecer nem piscar, como já fazem as listas.
- **Testes:** séries com dias vazios, período anterior, filtro por agente, exclusão de ocultas, top N, ferramentas, migration 0003 com reindexação e tempo da consulta no índice de teste grande.

### 6. Verificação

- `make check` e `make build`.
- Capturas de cada tela no modo servidor (Chrome), comparadas com a maquete.
- Rolagem do viewer e da lista longa sem travar.
- `make release` e `make test-install`: as capturas dos contêineres mostram o tema novo.

## Decidido pelo usuário

- Paleta e estilo **B · Aurora**, em todas as telas.
- Dashboard com os painéis analíticos e dados novos (opção 1).
- Abordagem: tokens + superfícies em camadas + SVG próprio + serviço de estatísticas.

## Decisões minhas (para conferir)

- Desfoque só nas peças flutuantes; cards translúcidos sem desfoque (desempenho).
- Inter e JetBrains Mono empacotadas; a Geist sai.
- Período padrão de 30 dias; métrica padrão do gráfico: Mensagens.
- Coluna `tools` com reindexação, em vez de contar pelo texto indexado.
- "Projetos fixados" = top 3 por atividade (não há fixação manual).
- "Aberta" = escrita no último minuto (mesma regra da retomada).
- "Mensagem" = mesma regra de `message_count` (só com texto), com o marcador `has_text` gravado pela migration 0003.
- Só Sessões e Mensagens trocam o gráfico; Favoritas usam `favorites.created_at` para o mini-gráfico e a variação.
- O card de Providers vira legenda com estado na rosca "Por agente"; o diagnóstico completo fica em Configurações.
- "Tudo" agrupa por semana (até 2 anos) ou por mês, sem período anterior.
- Abas da tabela vêm do `StatsService` (5 por aba, com os filtros).
- Nomes de ferramenta crus, sem equivalência entre providers.
- "Retomar última" pula sessões abertas e respeita o filtro de agente.
- Filtros valem para todos os números, inclusive os totais.

## Deixado ao implementador

- Detalhes de espaçamento e tamanhos finos, seguindo a maquete.
- Tema de cores do destaque de código no viewer, combinando com a paleta.
- Clique num dia do gráfico: se a lista de Sessões ainda não filtra por data, acrescentar um filtro de data na consulta da listagem. Se isso crescer demais, o clique abre a lista sem filtro e a pendência vai para o ROADMAP.
