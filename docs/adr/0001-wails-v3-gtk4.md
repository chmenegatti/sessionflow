# ADR 0001 — Wails v3 com GTK4 + WebKitGTK 6.0

- **Status:** aceito · 2026-11-21

## Contexto
O CLAUDE.md pede Wails v3 "após validar". A Fase 0 confirmou que a v3 é estável (v3.0.0 em 2026-10-26; v3.1.0 em 2026-11-01). O backend Linux padrão é GTK4 + WebKitGTK 6.0; GTK3 está depreciado.

## Decisão
- Wails **v3.1.0** fixado no `go.mod` e no `@wailsio/runtime` (mesma versão).
- Backend Linux padrão (GTK4 / `libwebkitgtk-6.0-dev`), sem `-tags gtk3`.
- `toolchain go1.26.11` no `go.mod` (correções de crypto/x509, recomendada pelo Wails, e de net/http GO-2026-6859).
- Template `react` (TypeScript), sem os alvos Android e iOS.
- `Linux.ApplicationID = com.cesarmenegatti.sessionflow`, que um eventual build Flatpak exigirá.

## Consequências
- Requer `libgtk-4-dev` e `libwebkitgtk-6.0-dev` para compilar.
- Atualizar o Wails significa trocar a versão no `go.mod` e no `package.json` juntos.
- Advisory GHSA-2p5f-2gx8-mh8m: conteúdo de sessão nunca é renderizado como HTML cru nem em iframe.
