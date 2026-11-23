#!/usr/bin/env bash
# Imprime a seção da versão $1 do CHANGELOG.md (notas do GitHub Release).
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/../.." && pwd)
awk -v v="$1" '
  $0 ~ "^## \\[" v "\\]" { on = 1; next }
  on && /^## \[/ { exit }
  on && /^\[[^]]+\]: / { exit }
  on { print }
' "$ROOT/CHANGELOG.md"
cat <<NOTES

---
Instalação e verificação: [docs/INSTALL.md](https://github.com/chmenegatti/sessionflow/blob/v$1/docs/INSTALL.md).
Confira os arquivos com \`sha256sum -c SHA256SUMS\`.
NOTES
