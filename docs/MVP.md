# MVP

## Escopo
Dashboard · Explorador de projetos · Explorador de sessões (filtros, favoritos, tags) · Viewer (markdown, código, tool calls recolhíveis, carregamento incremental) · Busca global FTS5 · Retomada pela CLI · Sincronização incremental com fsnotify.

## Critérios de aceite
| Critério | Situação |
|---|---|
| Detectar os três providers | ✔ diretório, formato e CLI (Fase 2) |
| Listar sessões sem duplicação | ✔ listagem unificada com filtros e ordenação (Fase 3) |
| Agrupar por projeto | ✔ Projetos → sessões do projeto, de todos os providers (Fase 3) |
| Visualizar conversas | ✔ viewer com Markdown, código, tool calls/resultados, metadados, imagens e anexos; leitura em streaming e virtualização (Fase 4) |
| Pesquisar conteúdo | ✔ busca global FTS5 com palavras, frases, filtros, trechos destacados e navegação até a mensagem (Fase 5) |
| Retomar sessões | ✔ pela CLI original num terminal, sem shell; validado com Claude Code, Codex e OpenCode (Fase 6) |
| Atualizar históricos automaticamente | ✔ fsnotify + reconciliação periódica; criação, alteração e exclusão de arquivos (Fase 6) |
| Funcionar offline | ✔ sem acesso à rede |
| Não modificar os arquivos originais | ✔ leitura só; testado (diretório do provider somente leitura, sem `-shm` criado) |
| Recuperar o índice após corrupção | ✔ testado (lixo, arquivo truncado, esquema futuro) |
| Continuar funcionando com falha de um provider | ✔ testado (erro e panic em cada etapa) |
| Distribuição Linux | ✔ AppImage, .deb, .rpm e tar.gz instalados e executados em ambientes limpos (Fase 7, docs/INSTALL.md) |

## Fora do MVP
Flatpak (descartado, ADR 0009) · ramificações da árvore de mensagens · busca por substring (trigram) · leitura do formato JSON legado do OpenCode (< 1.2) · Windows/macOS.
