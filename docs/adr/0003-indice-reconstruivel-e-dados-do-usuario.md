# ADR 0003 — Dois bancos: índice reconstruível e dados do usuário

- **Status:** aceito · 2026-11-21

## Contexto
Critério de aceite: "recuperar o índice após corrupção". Favoritos e tags não podem ser reconstruídos a partir dos históricos. O CLAUDE.md §8 proíbe guardar cópias integrais dos JSONL.

## Decisão
- `index.db`: sessões, projetos, mensagens (só o texto pesquisável), FTS5 e `sync_state`. Se o `quick_check` falhar, se o `integrity-check` do FTS falhar ou se o esquema for mais novo que o binário, o arquivo é movido para `index.db.corrupt-<ts>` e recriado vazio. Só contam como corrupção os códigos `SQLITE_CORRUPT` e `SQLITE_NOTADB`; erro de permissão ou de disco não descarta o índice.
- `user.db`: favoritos e tags, com chave `(provider, native_id)`. Nunca é movido nem apagado automaticamente; um erro impede o app de subir.
- Configuração (`providers`, `settings`) fica em `config.json`, não em tabela, porque precisa existir antes dos bancos.
- Não existe tabela `content_blocks`. O viewer relê a fonte original sob demanda.

## Consequências
- Apagar `index.db` é sempre seguro.
- O viewer depende de a fonte original ainda existir (o Claude apaga transcripts após 30 dias por padrão). Sessão removida no provider sai do índice.
