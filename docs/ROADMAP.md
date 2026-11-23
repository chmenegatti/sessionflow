# Roadmap

| Fase | Escopo | Estado |
|---|---|---|
| 0 Discovery | formatos dos providers, Wails v3, FTS5, distribuição | ✔ 2026-11-21 · [relatório](discovery/00-relatorio.md) |
| 1 Foundation | projeto Wails v3 + React/TS/Vite/Tailwind/shadcn, SQLite + migrations, slog, config, layout, tema dark | ✔ 2026-11-21 |
| 2 Provider Integration | leitor JSONL e leitor SQLite seguro; adaptadores Claude → Codex → OpenCode, com testes sobre os fixtures; indexação de metadados e texto | ✔ 2026-11-22 |
| 3 Session Management | listagem, agrupamento, persistência, favoritos, tags | ✔ 2026-11-22 |
| 4 Viewer | normalização, markdown, código, tool calls, paginação | ✔ 2026-11-22 |
| 5 Search | indexação, filtros, snippets, navegação até a mensagem | ✔ 2026-11-22 |
| 6 Resume and Watcher | `terminal.Launcher`, fsnotify, sync incremental, eventos Wails | ✔ 2026-11-22 |
| 7 Release | AppImage, .deb, .rpm, tar.gz, GitHub Releases (workflow), testes de instalação em contêineres limpos | ✔ 2026-11-22 · 0.1.0 |
| 8 Analytics: consumo e estatísticas | tokens por agente, modelo, projeto, sessão e origem (deduplicados, sem tratar ausência como zero); atividade com as regras do Dashboard; telas Visão geral, Consumo e Atividade · [ANALYTICS.md](ANALYTICS.md) | ✔ 2026-11-22 |
| 9 Analytics: custos e padrões | custo estimado por resposta com preços locais, revisões e vigência (estimativas preservadas, Recalcular explícito); relatórios de custo por agente, modelo, projeto, sessão e período; padrões de atividade (sem medir produtividade); exportação CSV; Vitest · [ANALYTICS.md](ANALYTICS.md) | ✔ 2026-11-23 |
| Hardening pós-Fase 9 | prévia do Recalcular com token; encerrar vigência sem sucessor; rankings por moeda; legenda em decimal; testes de concorrência com interleaving controlado; dias seguidos por data civil; CSV com BOM e proteção ampliada; linha do tempo de consumo da sessão | ✔ 2026-11-23 |

## Pendências conhecidas
- **Analytics** (ficaram fora da Fase 9):
  - vínculo de subagentes do Codex (`parent_thread_id`) para a origem e a consolidação por sessão;
  - filtro por data na listagem de sessões (o clique no gráfico abre `/sessions` sem filtro);
  - fuso com horário de verão nas agregações por intervalo (gráficos de Atividade, Consumo e Custos ainda usam um deslocamento só para o período; os dias de Padrões já usam o do instante);
  - confirmar se o reasoning do OpenCode está dentro da saída quando houver registros com reasoning > 0;
  - custos: preço separado para cache de 5 min e de 1 h do Claude; descontos (lote, faixas, contexto longo); `Refresh` incremental por sessão alterada se o volume crescer (hoje 197 ms com 32,9 mil respostas);
  - padrões: pausa da retomada configurável.
- **Bundle**: `motion` acrescentou ~156 KB ao chunk principal (190 KB gzip no total). Se pesar, trocar por `LazyMotion` + `m` com `domMax`.
- **Reindexação por fonte inteira**: arquivo alterado é relido do início (JSONL) e o banco do OpenCode inteiro é relido a cada mudança. Na Fase 6 o watcher limita o custo (só o provider alterado; arquivo ≥ 8 MB no máximo a cada 30 s), mas uma sessão ativa grande ainda custa uma releitura completa a cada 30 s (~1,5 s de CPU para 150 MB). Leitura a partir do offset exige retomar o estado do parser (agrupamento por `message.id`, numeração) e fica para depois.
- **Primeira indexação** de 2 GB leva ~22 s (decodificação JSON). Roda em segundo plano; aceitável no MVP.
- **Codex**: ligação de subagentes (`parent_thread_id`) e título de `state_*.sqlite` não implementados (formato não verificado localmente).
- **Claude**: subagentes ficam como sessões filhas (`parent_native_id`), ocultos por padrão na listagem.
- **OpenCode `-shm`**: a leitura `mode=ro` atualiza marcas de leitura no `-shm` existente. Se isso não for aceitável, trocar por cópia sempre (custo: copiar o banco a cada mudança).
- **Memória**: linhas JSONL enormes (até 32 MB, imagens em base64) custam ~4× o tamanho da linha durante a decodificação; pico medido 136 MB.
- **Backups `index.db.corrupt-*`** nunca são limpos e contêm texto de conversas. Manter só o mais recente (Fase 2 ou 6).
- **Log** só rotaciona na abertura; uma sessão longa em debug cresce sem limite.
- Atalho `Ctrl+F` (buscar na conversa) não implementado. O viewer é virtualizado, então a busca do navegador não alcança linhas fora da tela; `Ctrl+F` pode reusar o `SearchService` filtrado pela sessão e `OpenAt`.
- Busca → viewer: `OpenAt` não envia as mensagens anteriores ao alvo, mas o parse ainda lê a fonte até ele; custo proporcional à posição (checkpoints de offset descartados por ora: maior sessão local abre no fim em poucos segundos).
- Busca: o termo não é destacado dentro da conversa (só a mensagem alvo, com os blocos recolhidos abertos). Sem busca por substring no meio da palavra (trigram, ADR 0007). Palavras com 1–2 letras casam só inteiras. Mensagens de sistema, metadados e raciocínio não são indexados; resultados de ferramenta entram até 4 KB e mensagens até 16 KB.
- Busca: a paginação para em ~1000 resultados (o total é contado até 1001); refinar a consulta é o caminho para além disso. Total e página são consultas separadas: com a indexação rodando, podem divergir em uma página (o frontend descarta repetidos).
- Viewer: blocos acima de 64 KB e originais acima de 8 KB aparecem cortados; imagens do histórico não são exibidas (só a referência).
- Viewer: ao rolar, linhas recém-medidas podem deslocar o conteúdo em algumas dezenas de pixels (ajuste de estimativa da virtualização); medido até ~100 px.
- Configurações: diretório de dados e habilitação de provider valem só ao reiniciar; `logLevel` continua só no `config.json`.
- Retomada: o terminal é detectado entre emuladores conhecidos; outros exigem configurar caminho e argumentos. Sintaxes de kgx, konsole, kitty, alacritty, wezterm, foot, ghostty e xfce4-terminal vêm da documentação de cada um (só ptyxis e gnome-terminal foram executados). Flatpak (`flatpak-spawn --host`) não suportado.
- Retomada: a detecção de "sessão ativa" é heurística (alteração há menos de 1 min); não há trava entre processos. As CLIs gravam no histórico ao abrir uma sessão (docs/PROVIDERS.md §Retomada), então tentar retomar e cancelar ainda marca a sessão como alterada.
- Retomada: se a sessão passar a "ativa" entre o plano e a execução (segundos), o usuário vê o erro em vez da confirmação; basta repetir.
- Flatpak descartado (ADR 0009); o que foi medido ao tentar fica registrado lá.
- Sem atualização automática dentro do app: novas versões por download (atualização autorizada no milestone 0.3.0).
- Linux só x86_64 (arm64 exigiria builder arm64: o CGO do WebKit não cruza fácil); Windows só x64.
- Sem assinatura paga: SmartScreen no Windows e Gatekeeper no macOS pedem confirmação na primeira abertura.
- Viewer aberto não acompanha a conversa em andamento (o cursor lê a fonte uma vez); listas, busca e detalhes acompanham pelo evento `index:updated`.
- Sem gerenciamento global de tags (renomear/apagar): tags somem quando ficam sem uso.
- Pesquisa por título na listagem (CLAUDE.md §7.3) continua pendente: o FTS indexa o texto das mensagens (o primeiro prompt costuma conter o assunto), não o título da sessão.
- Testes de frontend (Vitest, Fase 9) cobrem a lógica de apresentação (`money.ts`, `format.ts`, `costFormat.ts`, `toggleProviders`); componentes não têm teste.
- Clicar num dia do gráfico de atividade do Dashboard abre `/sessions` sem filtro: a listagem ainda não filtra por data. Pendente um filtro de período em `SessionQuery`.
- `summary.ts` do Dashboard ainda sem teste (Vitest já disponível).
