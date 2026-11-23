-- Fase de redesign (Dashboard analítico): por mensagem, se conta como
-- "mensagem" (domain.CountsAsMessage, mesma regra de sessions.message_count)
-- e as ferramentas chamadas (array JSON, com repetição). Limpar o estado
-- força a releitura, que preenche as colunas novas.
ALTER TABLE messages ADD COLUMN has_text INTEGER NOT NULL DEFAULT 0;
ALTER TABLE messages ADD COLUMN tools TEXT NOT NULL DEFAULT '[]';
CREATE INDEX messages_ts_idx ON messages (ts, has_text, session_id);
DELETE FROM sync_state;
