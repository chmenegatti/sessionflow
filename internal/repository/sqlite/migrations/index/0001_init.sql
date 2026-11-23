-- Índice reconstruível (docs/discovery/05-sqlite-fts5.md).
-- Timestamps em milissegundos Unix (UTC).

CREATE TABLE projects (
  id   INTEGER PRIMARY KEY,
  path TEXT    NOT NULL UNIQUE,  -- caminho absoluto normalizado
  name TEXT    NOT NULL
);

CREATE TABLE sessions (
  id               INTEGER PRIMARY KEY,
  provider         TEXT    NOT NULL,
  native_id        TEXT    NOT NULL,
  parent_native_id TEXT,                        -- subagente / subsessão
  project_id       INTEGER REFERENCES projects(id) ON DELETE SET NULL,
  title            TEXT    NOT NULL DEFAULT '',
  first_prompt     TEXT    NOT NULL DEFAULT '',
  model            TEXT    NOT NULL DEFAULT '',
  created_at       INTEGER,
  updated_at       INTEGER,
  message_count    INTEGER NOT NULL DEFAULT 0,
  source_kind      TEXT    NOT NULL,            -- jsonl | sqlite
  source_path      TEXT    NOT NULL,
  archived         INTEGER NOT NULL DEFAULT 0,
  automated        INTEGER NOT NULL DEFAULT 0,
  UNIQUE (provider, native_id)
);
CREATE INDEX sessions_updated_idx ON sessions (updated_at DESC);
CREATE INDEX sessions_project_idx ON sessions (project_id, updated_at DESC);
CREATE INDEX sessions_source_idx  ON sessions (provider, source_path);

CREATE TABLE messages (
  id          INTEGER PRIMARY KEY,            -- rowid usado pelo FTS
  session_id  INTEGER NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  seq         INTEGER NOT NULL,               -- ordem física na fonte
  native_id   TEXT,
  role        TEXT    NOT NULL,
  ts          INTEGER,
  search_text TEXT    NOT NULL DEFAULT '',    -- texto indexável, já truncado
  UNIQUE (session_id, seq)
);

-- FTS5 em modo external content: não duplica o texto de messages.
CREATE VIRTUAL TABLE messages_fts USING fts5(
  search_text,
  content = 'messages',
  content_rowid = 'id',
  tokenize = 'unicode61 remove_diacritics 2'
);

-- Triggers mantêm o FTS sincronizado (padrão da documentação FTS5 §4.4.3).
CREATE TRIGGER messages_ai AFTER INSERT ON messages BEGIN
  INSERT INTO messages_fts (rowid, search_text) VALUES (new.id, new.search_text);
END;
CREATE TRIGGER messages_ad AFTER DELETE ON messages BEGIN
  INSERT INTO messages_fts (messages_fts, rowid, search_text) VALUES ('delete', old.id, old.search_text);
END;
CREATE TRIGGER messages_au AFTER UPDATE OF search_text ON messages BEGIN
  INSERT INTO messages_fts (messages_fts, rowid, search_text) VALUES ('delete', old.id, old.search_text);
  INSERT INTO messages_fts (rowid, search_text) VALUES (new.id, new.search_text);
END;

-- Progresso de sincronização por fonte (arquivo JSONL ou banco do provider).
CREATE TABLE sync_state (
  provider    TEXT    NOT NULL,
  source_path TEXT    NOT NULL,
  size        INTEGER NOT NULL DEFAULT 0,
  mtime_ns    INTEGER NOT NULL DEFAULT 0,
  cursor      TEXT    NOT NULL DEFAULT '',  -- opaco: offset+fingerprint (JSONL) ou marca d'água (SQLite)
  status      TEXT    NOT NULL DEFAULT 'ok',-- ok | partial | error | unsupported
  error       TEXT,                         -- nunca contém conteúdo de conversa
  updated_at  INTEGER NOT NULL,
  PRIMARY KEY (provider, source_path)
);
