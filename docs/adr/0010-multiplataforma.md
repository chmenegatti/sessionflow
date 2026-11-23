# ADR 0010 — Windows e macOS

**Status:** aceito · 2026-11-23 (0.2.0). Design: `docs/superpowers/specs/2026-11-23-multiplatform-update-design.md`.

## Contexto
O SessionFlow é público e o pedido é distribuir para Windows e macOS, além do Linux, sem certificado pago, mantendo as regras do projeto (sem shell, sem tocar nos históricos, offline).

## Decisão
- **Uma base, diferenças por GOOS.** Funções que recebem o sistema (`config.ResolvePaths`, `config.DefaultProviderRoot`, `provider.CLICandidates`, `terminal.Resolve`) e arquivos `_windows.go` só para chamadas do sistema (lock com `LockFileEx`, `CREATE_NEW_CONSOLE`, `CREATE_NEW_PROCESS_GROUP`). Tudo testável em qualquer SO por tabela; o CI roda nos três.
- **Pastas no padrão de cada sistema:** `%APPDATA%`/`%LOCALAPPDATA%` no Windows, `~/Library` no macOS, XDG no Linux.
- **Windows:** instalador NSIS **por usuário** (`%LOCALAPPDATA%\Programs\SessionFlow`, chave em `HKCU`, sem UAC — atualizar não pede administrador) e `.zip` portátil (mesmas pastas de dados). Sem assinatura Authenticode.
- **macOS:** app universal (`lipo`) com assinatura **ad-hoc**, `.dmg` e `.zip` do `.app`. Sem notarização.
- **Retomada sem shell em todos:** Windows Terminal (`wt -d <dir> -- <cmd>`, recusando `;`) ou console nova; no macOS, só emuladores que recebem argv (kitty, WezTerm, Alacritty, Ghostty). CLI `.cmd`/`.ps1` do npm e macOS só com Terminal.app viram *Copiar comando*.
- **Caminho de projeto POSIX** (`/...`) vale em qualquer sistema; do Windows (`C:\...`), só no Windows.
- **Sem Flatpak** (ADR 0009).

## Consequências
- SmartScreen e Gatekeeper pedem confirmação na primeira abertura (documentado no README e no INSTALL).
- Interface, retomada e instalador só se confirmam à mão: cada release passa por `docs/manual-tests/<versão>.md` nos dois sistemas antes da tag.
