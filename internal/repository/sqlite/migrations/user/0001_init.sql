-- Dados do usuário: NÃO reconstruíveis a partir dos históricos.
-- Referenciam sessões pela chave estável (provider, native_id), não pelo id
-- do índice, para sobreviver a um rebuild do index.db.

CREATE TABLE favorites (
  provider   TEXT    NOT NULL,
  native_id  TEXT    NOT NULL,
  created_at INTEGER NOT NULL,
  PRIMARY KEY (provider, native_id)
);

CREATE TABLE tags (
  id   INTEGER PRIMARY KEY,
  name TEXT    NOT NULL UNIQUE COLLATE NOCASE
);

CREATE TABLE session_tags (
  tag_id    INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
  provider  TEXT    NOT NULL,
  native_id TEXT    NOT NULL,
  PRIMARY KEY (tag_id, provider, native_id)
);
CREATE INDEX session_tags_session_idx ON session_tags (provider, native_id);
