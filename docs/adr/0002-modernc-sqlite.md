# ADR 0002 — Driver SQLite `modernc.org/sqlite`

- **Status:** aceito · 2026-11-21

## Contexto
É preciso FTS5. `mattn/go-sqlite3` só tem FTS5 com a build tag `sqlite_fts5`, que precisaria estar em todo caminho de build (Taskfile, geração de bindings, CI). `modernc.org/sqlite` é puro Go e já vem com FTS5. `ncruces/go-sqlite3` exige registrar a extensão.

## Decisão
`modernc.org/sqlite` v1.62.0 via `database/sql` (driver `"sqlite"`). Os PRAGMAs vão no DSN (`_pragma=`).

## Consequências
- Nenhuma tag de build; não aumenta a superfície CGO.
- Desempenho menor que o do mattn. Reavaliar com benchmark na Fase 5 se a busca ficar lenta.
- FTS5 validado na Fase 0 (sem acento, frase, prefixo, highlight) e coberto por testes.
