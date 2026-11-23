# ADR 0005 — Contrato dos providers e indexação por fonte

**Status:** aceito · 2026-11-22 (Fase 2)

## Contexto
O CLAUDE.md §6 sugere `SessionProvider{Detect, Discover, ReadSession, Resume}`. Na prática os três formatos diferem: Claude e Codex têm um JSONL por sessão (até 172 MB), o OpenCode tem um banco SQLite com todas as sessões. Carregar uma sessão inteira em memória (`Discover` devolvendo `[]Session` com mensagens) viola "evitar processamento integral de arquivos grandes".

## Decisão
- Contrato `provider.Provider`: `Detect` (nunca falha; problemas viram `Status`), `Sources` (arquivos ou bancos) e `Parse(src, Sink)`, que entrega mensagens em streaming e os metadados da sessão ao fim.
- A unidade de sincronização é a **fonte**, não a sessão. Fonte inalterada (tamanho, mtime, `Fingerprint` do `-wal`) é pulada; fonte alterada é relida e substituída numa transação.
- `Resume` sai do provider e vira montagem de comando na Fase 6 (`terminal.Launcher`), como previsto na discovery.
- Persistência fica no `indexer`; adaptadores não conhecem o SQLite do app.
- O índice guarda só o texto pesquisável (truncado). O viewer (Fase 4) usará o mesmo `Parse` para reler a fonte.

## Consequências
- Um adaptador novo implementa três métodos e testa contra fixtures, sem banco.
- Arquivo que cresce é relido do início. Aceitável no MVP (incremental 39 ms quando nada muda); offset incremental fica para a Fase 6.
- Mesma sessão em duas fontes (thread revertida do Codex) fica com a primeira lida; contada em `duplicates`.
