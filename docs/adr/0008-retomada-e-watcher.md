# ADR 0008 — Retomada pela CLI num terminal e sincronização por watcher

**Status:** aceito · 2026-11-22 (Fase 6)

## Contexto
As três CLIs retomam sessões de forma interativa (TUI): precisam de um terminal. O comando inclui dados vindos do histórico (ID, diretório), que não podem virar código. A indexação era feita só ao abrir o app; uma sessão ativa grava a cada poucos segundos e a fonte é relida inteira quando muda.

## Decisão
- O adaptador só monta argumentos (`provider.Resumer`); o serviço valida a sessão, o executável e o diretório; o `terminal.ExecLauncher` executa o emulador com argv separado, sem shell, em sessão própria (`Setsid`).
- ID fora do formato do provider é recusado (nada de argumento começando com `-`). Diretório inexistente bloqueia em vez de cair num substituto.
- Sintaxe por emulador conhecido, ou modelo do usuário com `{dir}`/`{cmd}` como itens do argv. Emulador que altera argumentos (ptyxis expande `$VAR`) recusa argv com `$`.
- Sessão alterada há menos de 1 min pede confirmação (risco de dois processos na mesma sessão).
- fsnotify com espera por silêncio aciona indexações parciais (só providers alterados) com limite de releitura para arquivos grandes. Reconciliação completa periódica cobre eventos perdidos, limite de inotify e raízes criadas depois.
- Offset incremental (ler só o acréscimo) adiado: exige estado de parser persistido.

## Consequências
- Nenhum dado de sessão passa por shell; o teste do launcher prova argv intacto com `;`, `$()`, espaços e aspas, e a validação real com ptyxis e gnome-terminal confirmou.
- Retomar depende de um emulador instalado (detectado ou configurado). Flatpak exigiria `flatpak-spawn --host`.
- Sessão ativa grande custa uma releitura completa a cada 30 s enquanto grava.
