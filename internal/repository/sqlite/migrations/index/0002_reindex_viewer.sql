-- Fase 4: os adaptadores passaram a emitir registros antes descartados
-- (metadados), o que muda a numeração (seq) das mensagens. Limpar o estado
-- força a releitura de todas as fontes na próxima indexação, mantendo seq
-- igual entre índice e viewer.
DELETE FROM sync_state;
