# 05 — SQLite + FTS5: driver e indexação incremental

> Fase 0 · 2026-11-21 · Legenda: **[DOC]** doc oficial · **[SRC]** código-fonte · **[EXP]** experimento executado nesta máquina · **[NÃO VERIFICADO]**

## 1. Escolha do driver

| Driver | CGO | FTS5 | Origem |
|---|---|---|---|
| `mattn/go-sqlite3` v1.14.53 | sim | só com build tag `sqlite_fts5` (padrão = FTS3) | [SRC] `sqlite3_opt_fts5.go` |
| **`modernc.org/sqlite` v1.62.0** | **não** | **embutido** (`-DSQLITE_ENABLE_FTS5`) | [SRC] `lib/sqlite_linux_amd64.go` |
| `ncruces/go-sqlite3` v0.38.0 | não (wasm2go) | via `ext/fts5` + `fts5.Register(conn)` | [SRC] |

**Recomendação: `modernc.org/sqlite`** via `database/sql` (driver `"sqlite"`).
- FTS5 sem build tags → nenhum caminho de build (Taskfile, `wails3 generate bindings`, CI) pode esquecer a tag.
- Puro Go → não amplia a superfície CGO além do que o Wails já exige; compatível com eventual `-tags purego`.
- Trade-off: mais lento que mattn. Volume esperado (centenas de milhares de mensagens) não justifica CGO extra. Reavaliar só se benchmark da Fase 5 indicar gargalo.

### Experimento [EXP]
Programa em scratch com `modernc.org/sqlite v1.62.0` (SQLite **3.54.1** embutido):

| Teste | Consulta | Resultado |
|---|---|---|
| Sem acento | `migracao` com `tokenize='unicode61 remove_diacritics 2'` | encontrou "**Migração** do repositório" |
| Frase | `"retomar sessão"` | encontrou a frase exata |
| Prefixo | `Read*` | encontrou `ReadSession` |
| Substring (trigram) | `adSess` com `tokenize='trigram'` | encontrou `Re[adSess]ion` |
| Booleano | `sqlite NOT claude` | ok |
| Destaque | `highlight(t,0,'[',']')` | ok |
| WAL | `_pragma=journal_mode(WAL)` no DSN | ok |

## 2. Esquema FTS proposto

```sql
-- conteúdo canônico (texto já normalizado e truncado para busca)
CREATE TABLE messages (
  id          INTEGER PRIMARY KEY,          -- rowid estável usado pelo FTS
  session_id  INTEGER NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  seq         INTEGER NOT NULL,             -- ordem dentro da sessão
  source_id   TEXT,                          -- uuid/msg_id/call_id original
  role        TEXT NOT NULL,
  ts          INTEGER,
  search_text TEXT NOT NULL,                -- texto indexável (sem base64, sem saídas gigantes)
  UNIQUE(session_id, seq)
);

-- FTS5 external content: não duplica o texto
CREATE VIRTUAL TABLE messages_fts USING fts5(
  search_text,
  content='messages', content_rowid='id',
  tokenize='unicode61 remove_diacritics 2'
);
```

- Filtros por provider/projeto: `JOIN messages m ON m.id = messages_fts.rowid JOIN sessions s ...` — filtros ficam fora do FTS (colunas UNINDEXED não são necessárias).
- Navegação até a mensagem: `rowid → messages.session_id + seq`.
- Destaque: `snippet(messages_fts, 0, '<mark>', '</mark>', '…', 12)` — o front renderiza `<mark>` com escape do resto (nunca HTML cru).
- Trigram (busca por substring em código) **fora do MVP**: dobra o índice. Opcional na Fase 5 se a busca por prefixo não bastar.

## 3. Estratégia de indexação incremental

### Unidade de sincronização = arquivo/fonte
Tabela `sync_state` por fonte:

```sql
CREATE TABLE sync_state (
  provider    TEXT NOT NULL,
  source_path TEXT NOT NULL,     -- arquivo JSONL ou "db:<path>#<session_id>"
  size        INTEGER NOT NULL,
  mtime_ns    INTEGER NOT NULL,
  offset      INTEGER NOT NULL,  -- bytes já consumidos (JSONL append-only)
  last_seq    INTEGER NOT NULL,
  fingerprint TEXT,              -- hash dos primeiros N bytes (detecta substituição)
  status      TEXT NOT NULL,     -- ok | partial | error
  error       TEXT,              -- sem conteúdo de conversa
  PRIMARY KEY (provider, source_path)
);
```

### JSONL (Claude Code, Codex)
1. `stat` → igual a `size/mtime` salvos → pular.
2. `size` maior e `fingerprint` igual → **append**: `Seek(offset)`, ler linhas completas, indexar a partir de `last_seq+1`.
3. `size` menor ou `fingerprint` diferente → arquivo reescrito → **reindexar a sessão** (delete + insert na mesma transação).
4. Última linha sem `\n` = escrita em andamento → não consumir; `offset` para no último `\n`. Status `partial`.
5. Linha JSON inválida no meio → registrar contador (sem conteúdo), seguir.

### SQLite (OpenCode; metadados Codex)
- Sem offset de bytes. Usar `time_updated` (OpenCode `session`, `message`, `part`) e `updated_at_ms` (Codex `threads`) como marca d'água.
- Sessão com `time_updated` > marca → reindexar aquela sessão (sessões são pequenas relativo a JSONL de 100+ MB).
- Leitura **somente leitura** e sem tocar diretório original (ver §4).

### Transações e FTS
- Uma transação por sessão/lote: `INSERT messages` + `INSERT INTO messages_fts(rowid, search_text)`.
- Remoção em external content exige o comando especial: `INSERT INTO messages_fts(messages_fts, rowid, search_text) VALUES('delete', ?, ?)` com o texto antigo — ou usar triggers padrão da doc FTS5. **Preferir triggers** (menos chance de índice divergente). [DOC fts5 §4.4.3]
- Após reindex em massa: `INSERT INTO messages_fts(messages_fts) VALUES('optimize')`.

### Recuperação após corrupção (critério de aceite)
- Na abertura: `PRAGMA quick_check` + `INSERT INTO messages_fts(messages_fts) VALUES('integrity-check')`.
- Falhou → renomear `index.db` para `index.db.corrupt-<ts>`, recriar esquema, rescan completo. Favoritos e tags são os únicos dados não reconstruíveis → manter em **tabela/arquivo separado** (`user.db` ou tabela exportada em JSON) para sobreviver ao rebuild.
- `PRAGMA user_version` para migrações; versão desconhecida → rebuild.

## 4. Lendo bancos SQLite de terceiros sem modificá-los [EXP]

Teste com cópia do `opencode.db` (WAL):

| Cenário | `?mode=ro` | Efeito colateral |
|---|---|---|
| Diretório gravável, `-wal` presente, sem `-shm` | lê | **cria `-shm` no diretório** |
| Diretório só-leitura, `.db`+`-wal`+`-shm` presentes | lê | nenhum |
| Diretório só-leitura, sem `-shm` | `unable to open database file` | — |

Conclusão: abrir o banco original com `mode=ro` pode **criar arquivo** no diretório do provider (viola regra "nunca modificar"), e falha em montagem `:ro` (Flatpak). Estratégia:
1. Preferido: abrir com `file:<path>?mode=ro` **somente se `-shm` já existir** (provider rodando ou recém-fechado); senão, 2.
2. Snapshot: copiar `.db` + `-wal` (+ `-shm`) para `$XDG_CACHE_HOME/sessionflow/snapshots/` e abrir a cópia. Custo: tamanho do banco (MB). **[NÃO VERIFICADO]** consistência de cópia concorrente com escritor ativo — mitigar repetindo a cópia se `PRAGMA quick_check` falhar.

## 5. Volume observado [OBS]
- Claude Code: 426 transcripts top-level + 119 de subagentes; mediana 883 KB, p95 15 MB, **máximo 172 MB** → leitura obrigatoriamente em streaming (`bufio.Reader` com linhas longas; `bufio.Scanner` padrão estoura em 64 KB por linha).
- Indexar apenas texto útil: user/assistant text, entrada resumida de tool_use, saída de tool_result truncada (ex.: 4 KB). Excluir `thinking` assinado vazio, `encrypted_content`, imagens base64.

## Fontes
- https://www.sqlite.org/fts5.html (external content, triggers, `highlight`, `snippet`, `integrity-check`, trigram)
- https://www.sqlite.org/wal.html · https://www.sqlite.org/uri.html (`mode=ro`, `immutable`)
- https://pkg.go.dev/modernc.org/sqlite · https://pkg.go.dev/github.com/mattn/go-sqlite3 · https://pkg.go.dev/github.com/ncruces/go-sqlite3
