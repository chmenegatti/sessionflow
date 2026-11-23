#!/usr/bin/env bash
# Valida os pacotes de dist/ em contêineres limpos: cada um instala, roda
# --version/--check, abre a GUI num X virtual (com históricos de teste),
# testa a segunda instância e desinstala. Resultados em dist/test/<alvo>/.
# Uso: test-install.sh [dist] [alvo...]
set -euo pipefail
DIST=$(realpath "${1:-dist}"); shift || true
ROOT=$(cd "$(dirname "$0")/../.." && pwd)
VERSION=$(tr -d '[:space:]' < "$ROOT/VERSION")
FIX="$ROOT/docs/discovery/fixtures"

# alvo → imagem e modo
declare -A IMAGE=( [ubuntu-24.04-deb]=ubuntu:24.04 [debian-13-deb]=debian:13 [fedora-rpm]=fedora:latest
                   [debian-13-appimage]=debian:13 [fedora-appimage]=fedora:latest [ubuntu-22.04-appimage]=ubuntu:22.04
                   [ubuntu-24.04-tarball]=ubuntu:24.04 )
declare -A MODE=( [ubuntu-24.04-deb]=deb [debian-13-deb]=deb [fedora-rpm]=rpm [debian-13-appimage]=appimage
                  [fedora-appimage]=appimage [ubuntu-22.04-appimage]=appimage [ubuntu-24.04-tarball]=tarball )
TARGETS=("$@"); [ ${#TARGETS[@]} -gt 0 ] || TARGETS=(ubuntu-24.04-deb debian-13-deb fedora-rpm debian-13-appimage fedora-appimage ubuntu-22.04-appimage ubuntu-24.04-tarball)

# Home de teste com os fixtures nos layouts reais dos três providers.
SEED=$(mktemp -d)
trap 'rm -rf "$SEED"' EXIT
mkdir -p "$SEED/.claude/projects/-home-tester-projects-demo-app" "$SEED/.codex/sessions/2026/01/10" "$SEED/.local/share/opencode"
cp "$FIX/claude/session.jsonl" "$SEED/.claude/projects/-home-tester-projects-demo-app/11111111-1111-4111-8111-111111111111.jsonl"
cp "$FIX/codex/rollout.jsonl" "$SEED/.codex/sessions/2026/01/10/rollout-2026-01-10T12-00-00-22222222-2222-7222-8222-222222222222.jsonl"
python3 -I -c 'import sqlite3,sys; db=sqlite3.connect(sys.argv[2]); db.executescript(open(sys.argv[1]).read()); db.commit()' \
  "$FIX/opencode/schema-v1.sql" "$SEED/.local/share/opencode/opencode.db"

for t in "${TARGETS[@]}"; do
  out="$DIST/test/$t"; rm -rf "$out"; mkdir -p "$out"
  echo "### $t (${IMAGE[$t]})"
  # --privileged: o WebKitGTK cria o próprio sandbox (bubblewrap, user
  # namespaces, montagem de /proc), que o Docker bloqueia por padrão. Num
  # desktop isso funciona sem nada especial; o contêiner continua limpo.
  docker run --rm --network host --privileged \
    -e EXPECT_VERSION="$VERSION" -e HOST_UID="$(id -u)" -e HOST_GID="$(id -g)" \
    -v "$DIST":/dist:ro -v "$SEED":/seed:ro -v "$out":/out \
    -v "$ROOT/packaging/scripts/test-in-container.sh":/t.sh:ro \
    "${IMAGE[$t]}" bash /t.sh "${MODE[$t]}" > /dev/null 2>&1 || true
  grep -E '^(OK|FAIL|DISTRO)' "$out/result.txt" 2>/dev/null | sed 's/^/  /' || echo "  sem resultado"
done
