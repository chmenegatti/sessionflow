# ADR 0004 — Distribuição Linux: AppImage e .deb primeiro; Flatpak adiado

- **Status:** aceito · 2026-11-21

## Contexto
O tooling do Wails v3 gera AppImage, deb, rpm e AUR, mas não Flatpak. No Flatpak, retomar uma sessão exige `flatpak-spawn --host`, o que significa `--talk-name=org.freedesktop.Flatpak`, um escape do sandbox que o Flathub tende a rejeitar. Montagens `:ro` também atrapalham a leitura de SQLite em WAL (discovery 06).

## Decisão
O MVP sai em **AppImage + .deb** (e .rpm, praticamente sem custo). Flatpak fica para depois do MVP, distribuído por repositório próprio, e só depois que `terminal.Launcher` e a leitura por snapshot estiverem prontos.

## Consequências
- Sem sandbox: o app lê os diretórios dos providers e executa as CLIs do host diretamente.
- A abstração `terminal.Launcher` (Fase 6) já deve aceitar uma implementação com `flatpak-spawn`.
