# ADR 0006 — Viewer por cursor em streaming

**Status:** aceito · 2026-11-22 (Fase 4)

## Contexto
Transcripts chegam a 172 MB e 35 mil registros. O índice guarda só texto pesquisável (ADR 0003), então o viewer relê a fonte original. Ler a conversa inteira em memória — no backend ou no frontend — viola o requisito de não carregar históricos grandes integralmente. Paginação sem estado (reler até o offset a cada página) custaria O(n²) para percorrer uma conversa.

## Decisão
- Cursor com estado no backend: uma goroutine faz o parse uma única vez e entrega páginas por canal sem buffer; o parse fica bloqueado até a próxima página ser pedida. Limites: 8 cursores, 10 min ociosos, cancelamento no shutdown.
- Somente para frente. Reabrir lê do início.
- No frontend, lista virtualizada; só as 12 páginas mais recentes mantêm conteúdo e as anteriores viram esqueletos (a lista não encolhe, logo a rolagem não pula). Remover as páginas e compensar a rolagem foi descartado após medição: grupos de eventos cortados entre páginas mudavam de chave e o conteúdo saltava.
- Adaptadores emitem todos os registros (metadados inclusive) e só copiam o JSON original quando `Source.Raw` está ligado (viewer).

## Consequências
- Ler a conversa toda custa uma passada pela fonte; memória limitada a uma página no backend e à janela no frontend.
- Esqueletos custam ~100 bytes por registro (35 mil registros ≈ 3,5 MB). Voltar para mensagens antigas além da janela exige reabrir; ir direto a uma mensagem (busca, Fase 5) custa ler até ela.
- A numeração (`seq`) passou a incluir metadados; a migration 0002 força a releitura para índice e viewer concordarem.
