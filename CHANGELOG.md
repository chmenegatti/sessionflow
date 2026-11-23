# Changelog

Formato: [Keep a Changelog](https://keepachangelog.com/pt-BR/1.1.0/). Versões: [SemVer](https://semver.org/lang/pt-BR/). A versão vem do arquivo `VERSION`, entra no binário por `-ldflags` e aparece em `sessionflow --version`, nos pacotes e na interface.

## [Não lançado]

## [0.2.0] - pré-lançamento

### Adicionado
- **Windows 10/11 (x64)**: instalador por usuário (sem administrador, em `%LOCALAPPDATA%\Programs\SessionFlow`) e versão portátil em `.zip`. Dados em `%LOCALAPPDATA%\SessionFlow`.
- **macOS 13+**: app universal (Intel e Apple Silicon) em `.dmg` e `.zip`, com assinatura ad-hoc. Dados em `~/Library/Application Support/SessionFlow`.
- **Retomar no terminal** no Windows (Windows Terminal ou, sem ele, uma janela de console) e no macOS (kitty, WezTerm, Alacritty ou Ghostty). Sem shell intermediário, como no Linux.
- **Copiar comando**: quando a retomada direta não é possível (CLI instalada pelo npm no Windows, que é um script `.cmd`; só o Terminal.app no macOS), o botão copia o comando pronto para colar.
- `sessionflow --selftest`: autoteste sem janela, usado na verificação dos pacotes.

### Alterado
- Mensagem de caminho inválido nas Configurações: "informe um caminho absoluto (completo)", válida nos três sistemas.

## [0.1.0] - 2026-11-23

Primeira versão.

### Adicionado
- **Providers**: Claude Code, OpenAI Codex e OpenCode, cada um com adaptador próprio. Leitura em streaming, tolerante a arquivos malformados ou incompletos e sem modificar os originais. Bancos SQLite de terceiros são abertos sem criar arquivos no diretório deles.
- **Dashboard** com totais, sessões recentes, favoritas e situação dos providers.
- **Sessões e projetos**: listagem unificada, filtros por provider, ordenação por atividade, agrupamento por projeto, favoritos e tags (guardados à parte do índice; sobrevivem a uma reconstrução).
- **Viewer**: Markdown, destaque de sintaxe, chamadas e resultados de ferramentas recolhíveis, metadados com o JSON original, referências a imagens e anexos. Leitura incremental com virtualização para históricos de centenas de MB.
- **Busca global** (SQLite FTS5): palavras e frases, filtros, trechos com destaque e navegação direto para a mensagem (`Ctrl+K`).
- **Retomada** pela CLI original num terminal (`Ctrl+Enter`), sem shell. Valida executável, ID e diretório e pede confirmação se a sessão parece aberta.
- **Sincronização automática**: fsnotify, releitura só do provider alterado e reconciliação periódica que cobre eventos perdidos.
- **Configurações** editáveis: diretórios dos providers, executáveis das CLIs e terminal.
- **Distribuição**: AppImage, .deb, .rpm e tar.gz. Ícone e identidade visual próprios, AppStream metainfo, `sessionflow --version`, `--check` (diagnóstico) e `--test-terminal`.
- Instância única: abrir de novo traz a janela existente para a frente.
- Tela de erro quando a inicialização falha.
- **Dashboard analítico**: frase-resumo do período; filtros de período (7, 30, 90 dias, tudo) e de agente; indicadores de sessões, mensagens, projetos ativos e favoritas com variação contra o período anterior e mini gráfico; gráfico de atividade por agente com o período anterior tracejado; "Por agente" com a situação de detecção; projetos mais ativos e ferramentas mais usadas; sessões recentes em tabela (todas, abertas, favoritas); "Retomar última" no cabeçalho.
- **Projetos fixados** na barra lateral (os 3 mais ativos em 30 dias) e estado da sincronização no rodapé.
- **Analytics** (nova seção na barra lateral): consumo de tokens e uso dos agentes a partir dos históricos locais, em três telas (Visão geral, Consumo, Atividade) com filtros de período (hoje, 7/30/90 dias, este mês, mês anterior, tudo, personalizado), agente, projeto e modelo. Tokens por agente, modelo, projeto, sessão e origem (agente principal, subagentes, sessões automáticas), cache e reasoning; campos que o agente não informa aparecem como indisponíveis, nunca como zero; valores calculados aparecem como derivados. Atividade com mapa dia da semana × hora e as mesmas contagens do Dashboard.
- **Linha do tempo da sessão** (Consumo → Por sessão, ao expandir): consumo por intervalo com entrada, saída e cache, retomadas marcadas, dados parciais e registros sem horário identificados; soma igual ao total da sessão.
- **Custos estimados** (Analytics → Custos): custo que o consumo de tokens teria com os preços que você cadastra, sempre identificado como estimativa (não é o valor cobrado; assinaturas não são cobradas por token). Total por moeda com variação e cobertura, qualidade da estimativa (completas, parciais, sem preço, sem dados), evolução por dia/semana/mês, por agente, modelo, projeto, categoria de token e sessão (com link para a conversa), sessões sem custo calculável. Sem consulta de preços na internet e sem conversão de moeda.
- **Tabela de preços** (Custos → Preços): por agente e modelo (id ou prefixo `*`), moeda, unidade, entrada, saída, cache lido e criado, vigência e origem. Editar cria uma revisão nova; "novo preço a partir de" registra uma mudança no tempo; desativar e reativar; histórico com o número de estimativas que usam cada revisão. Estimativas já calculadas guardam o preço usado; **Recalcular** aplica os preços atuais ao recorte, com prévia.
- **Encerrar vigência** de um preço sem cadastrar sucessor (Custos → Preços): depois do último dia, sem outro preço vigente, as respostas ficam sem preço.
- **Padrões** (Analytics → Padrões): métricas de atividade observada, sem medir produtividade. Calendário, sessões iniciadas e retomadas, dias e semanas com atividade, dias seguidos, duração observada, mensagens por sessão, intervalo entre sessões, projetos mais ativos e recorrentes, projetos com mais de um agente e comparação entre agentes.
- **Exportar CSV** em Custos e Padrões (gerado localmente; sem títulos nem texto de conversas).

### Interface
- **Redesign "Aurora"** em todas as telas: fundo em gradiente, superfícies translúcidas com sombra e borda iluminada, vidro nas peças flutuantes, acento ciano e violeta, cor própria por agente, fontes Inter e JetBrains Mono, tema de destaque de código na mesma paleta, projetos em cartões, busca com campo destacado e viewer com bolhas.
- Filtros de agente em Sessões e Busca em pílulas: com o filtro vazio todas aparecem ligadas, e o último agente ligado não desliga.
- "Mensagem" tem uma definição única (usuário ou assistente com texto), usada nas contagens e nas estatísticas.

### Segurança, privacidade e consistência
- Offline, sem telemetria; logs sem conteúdo de conversas.
- Nunca lê credenciais dos providers (`.credentials.json`, `auth.json`, tabelas `account*`).
- **Recalcular** só grava se os dados ainda forem os da prévia; se uma sincronização ou mudança de preço aconteceu no meio, nada é gravado e a prévia é atualizada.
- CSV com **BOM UTF-8** (acentos corretos no Excel do Windows) e proteção contra fórmula que também cobre tabulação, CR e espaços antes de `=`, `+`, `-`, `@`.

[Não lançado]: https://github.com/chmenegatti/sessionflow/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/chmenegatti/sessionflow/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/chmenegatti/sessionflow/releases/tag/v0.1.0
