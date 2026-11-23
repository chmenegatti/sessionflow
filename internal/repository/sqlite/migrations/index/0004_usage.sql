-- Fase 8 (Analytics): consumo de tokens, um registro por resposta (Claude,
-- OpenCode) ou pedido (Codex), já deduplicado pelo adaptador. NULL = o
-- provider não informou o campo (nunca zero). A mesma resposta pode estar em
-- duas sessões (principal e subagente): a deduplicação entre sessões é feita
-- na consulta (usage_store.go). Limpar o estado força a releitura.
CREATE TABLE usage (
  id                    INTEGER PRIMARY KEY,
  session_id            INTEGER NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  response_id           TEXT    NOT NULL,
  seq                   INTEGER NOT NULL,
  ts                    INTEGER,
  model                 TEXT    NOT NULL DEFAULT '',
  input_tokens          INTEGER,
  output_tokens         INTEGER,
  cache_read_tokens     INTEGER,
  cache_creation_tokens INTEGER,
  reasoning_tokens      INTEGER,
  total_tokens          INTEGER,
  derived               TEXT    NOT NULL DEFAULT '',
  status                TEXT    NOT NULL,
  UNIQUE (session_id, response_id)
);
CREATE INDEX usage_ts_idx ON usage (ts, session_id);
CREATE INDEX usage_resp_idx ON usage (response_id, session_id, ts); -- cobre a deduplicação entre sessões
DELETE FROM sync_state;
