# AGENTS.md

Instruções para agentes de código neste repositório. As regras completas do produto estão em [CLAUDE.md](CLAUDE.md).

## Antes de mudar algo
1. Leia `docs/ARCHITECTURE.md`, `docs/DOMAIN.md` e o documento de discovery do provider envolvido.
2. Trabalhe somente na fase pedida (`docs/ROADMAP.md`). Não avance de fase sem ordem explícita.

## Comandos
```bash
make check               # vet + staticcheck + testes -race + typecheck + Vitest
cd frontend && npm test  # testes do frontend (Vitest)
go test ./...            # testes do backend (sem GUI)
go vet ./...
cd frontend && npm run typecheck
wails3 build             # build completo → bin/sessionflow (gera bindings + frontend)
wails3 dev               # desenvolvimento
```

## Regras
- **Use Wails v3** (`wails3`, `github.com/wailsapp/wails/v3`). Nunca use a API nem a CLI `wails` v2, mesmo que estejam instaladas.
- **Sem estado global no backend:** dependências explícitas nos construtores, wiring em `app.go`.
- **Regra de negócio fica no Go.** O frontend só apresenta e chama `lib/api.ts`.
- **Bindings** (`frontend/bindings`) são gerados e ficam fora do git. Depois de mudar um método exportado de serviço, rode `wails3 task common:generate:bindings`.
- **Migrations** são arquivos novos `NNNN_nome.sql`, sequenciais. Nunca edite uma migration já publicada.
- **`user.db` nunca é apagado automaticamente.** `index.db` pode ser.
- **Logs** sem conteúdo de conversa. Registre caminho, provider, contagem e tipo de erro.
- **Processos** executados sem shell: `exec.Command(bin, args...)`, nunca `sh -c`.
- **Históricos dos providers** são somente leitura. Não abra o SQLite de terceiros de modo que crie `-shm`/`-wal` no diretório deles.
- Novo tipo de registro de provider vira `BlockUnknown`, nunca erro.
- Testes acompanham todo código de backend. Os fixtures de providers ficam em `docs/discovery/fixtures/` e são sintéticos (`providertest.Fixture` resolve o caminho).
- **Adaptadores** implementam `provider.Provider` e só leem. Parse em streaming (`provider/jsonl`), nunca o arquivo inteiro em memória. SQLite de terceiro só via `provider/sqliteread`.
- Um provider não importa outro. Persistência fica no `indexer`, nunca no adaptador.
