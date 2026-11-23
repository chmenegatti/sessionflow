-- Preços e estimativas de custo (Fase 9). Ficam no user.db porque não são
-- reconstruíveis: a estimativa preserva o preço usado.

-- Uma revisão nunca muda depois de criada, exceto replaced_by (editada),
-- disabled_at (desativada) e o fechamento da vigência (valid_to/closed_at,
-- só quando ainda sem fim). Preços: decimal canônico por unit_tokens;
-- NULL = categoria sem preço.
CREATE TABLE price_revisions (
  id             INTEGER PRIMARY KEY,
  price_key      TEXT    NOT NULL,
  provider       TEXT    NOT NULL,
  model          TEXT    NOT NULL,
  currency       TEXT    NOT NULL,
  unit_tokens    INTEGER NOT NULL,
  input          TEXT,
  output         TEXT,
  cache_read     TEXT,
  cache_creation TEXT,
  valid_from     INTEGER NOT NULL,           -- ms UTC
  valid_to       INTEGER,                    -- ms UTC, exclusivo; NULL = sem fim
  source         TEXT    NOT NULL DEFAULT '',
  note           TEXT    NOT NULL DEFAULT '',
  created_at     INTEGER NOT NULL,
  replaced_by    INTEGER REFERENCES price_revisions(id),
  disabled_at    INTEGER,
  closed_at      INTEGER
);
CREATE INDEX price_revisions_model_idx ON price_revisions (provider, model);
CREATE INDEX price_revisions_key_idx ON price_revisions (price_key, created_at);

-- Uma estimativa por registro de consumo (inclusive cópias que a
-- deduplicação descarta), pela chave estável da resposta. Custos em nanos
-- (10⁻⁹ da moeda); NULL = não calculado.
CREATE TABLE cost_estimates (
  provider              TEXT    NOT NULL,
  native_id             TEXT    NOT NULL,
  response_id           TEXT    NOT NULL,
  revision_id           INTEGER REFERENCES price_revisions(id),
  currency              TEXT,
  unit_tokens           INTEGER,
  input_tokens          INTEGER,
  output_tokens         INTEGER,
  cache_read_tokens     INTEGER,
  cache_creation_tokens INTEGER,
  input_cost            INTEGER,
  output_cost           INTEGER,
  cache_read_cost       INTEGER,
  cache_creation_cost   INTEGER,
  total_cost            INTEGER,
  status                TEXT    NOT NULL,
  reasons               TEXT    NOT NULL DEFAULT '',
  usage_ts              INTEGER,
  calculated_at         INTEGER NOT NULL,
  PRIMARY KEY (provider, native_id, response_id)
) WITHOUT ROWID;
CREATE INDEX cost_estimates_revision_idx ON cost_estimates (revision_id);
