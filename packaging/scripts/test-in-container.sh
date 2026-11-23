#!/usr/bin/env bash
# Roda DENTRO de um contêiner limpo (ver test-install.sh). Instala o pacote,
# executa como usuário comum e registra evidências em /out.
# Uso: test-in-container.sh <deb|rpm|appimage|tarball>
set -uo pipefail
MODE=$1
OUT=/out
exec > >(tee "$OUT/run.log") 2>&1
step() { echo; echo "== $*"; }
fail() { echo "FALHA: $*"; echo "FAIL $*" >> "$OUT/result.txt"; }
ok() { echo "ok: $*"; echo "OK $*" >> "$OUT/result.txt"; }
: > "$OUT/result.txt"
# Arquivos de evidência ficam do usuário do host.
trap 'chown -R "${HOST_UID:-0}:${HOST_GID:-0}" "$OUT"' EXIT
. /etc/os-release; echo "Distro: $PRETTY_NAME"; echo "DISTRO $PRETTY_NAME" >> "$OUT/result.txt"

X_DEB="xvfb xauth dbus-x11 imagemagick x11-utils procps"
X_RPM="xorg-x11-server-Xvfb xorg-x11-xauth dbus-x11 ImageMagick xwininfo procps-ng util-linux"

step "instalação ($MODE)"
case $MODE in
  deb)
    apt-get update -qq && DEBIAN_FRONTEND=noninteractive apt-get install -y -qq /dist/sessionflow_*_amd64.deb $X_DEB > "$OUT/install.log" 2>&1 \
      && ok "apt install do .deb (dependências resolvidas pelo apt)" || { fail "apt install"; tail -20 "$OUT/install.log"; exit 1; }
    BIN=sessionflow ;;
  rpm)
    dnf install -y -q /dist/sessionflow-*.x86_64.rpm $X_RPM > "$OUT/install.log" 2>&1 \
      && ok "dnf install do .rpm (dependências resolvidas pelo dnf)" || { fail "dnf install"; tail -20 "$OUT/install.log"; exit 1; }
    BIN=sessionflow ;;
  appimage)
    # Base de qualquer desktop (OpenGL, fontes, X11/Wayland), que o
    # linuxdeploy de propósito não embute; GTK4 e WebKitGTK ficam de fora.
    if command -v apt-get >/dev/null; then
      apt-get update -qq && DEBIAN_FRONTEND=noninteractive apt-get install -y -qq --no-install-recommends $X_DEB \
        libgl1 libegl1 libgbm1 libharfbuzz0b libfontconfig1 libfreetype6 libx11-6 libx11-xcb1 libxkbcommon0 \
        libwayland-client0 libwayland-egl1 libdrm2 libgles2 libfribidi0 libgpg-error0 libcom-err2 fonts-dejavu-core > "$OUT/install.log" 2>&1
    else
      dnf install -y -q $X_RPM mesa-libGL mesa-libEGL mesa-libgbm harfbuzz fontconfig freetype libX11 libxkbcommon \
        libwayland-client libwayland-egl libdrm libglvnd-gles fribidi libgpg-error libcom_err dejavu-sans-fonts > "$OUT/install.log" 2>&1
    fi
    # Nenhuma lib de GTK4/WebKitGTK do sistema: tudo tem de vir da AppImage.
    if ldconfig -p | grep -q 'libwebkitgtk-6.0'; then fail "ambiente já tem WebKitGTK 6.0"; else ok "ambiente sem GTK4/WebKitGTK 6.0 instalados"; fi
    install -m755 /dist/SessionFlow-*-x86_64.AppImage /usr/local/bin/SessionFlow.AppImage
    # Contêiner sem FUSE: a AppImage se extrai e roda (o mesmo binário).
    export APPIMAGE_EXTRACT_AND_RUN=1
    BIN=/usr/local/bin/SessionFlow.AppImage
    (cd /tmp && "$BIN" --appimage-extract >/dev/null 2>&1)
    missing=$(ldd /tmp/squashfs-root/usr/bin/sessionflow 2>&1 | grep 'not found' | grep -o -E '(lib[^ ]+\.so[.0-9]*|GLIBC_[0-9.]+)' | sort -u | tr '\n' ' ')
    # Bibliotecas que a AppImage usa do sistema (o resto vem de dentro dela).
    ldd /tmp/squashfs-root/usr/bin/sessionflow 2>/dev/null | grep '=> /' | grep -v squashfs-root | awk '{print $1}' | sort -u > "$OUT/system-libs.txt"
    for x in /tmp/squashfs-root/usr/lib/*/webkitgtk-6.0/WebKitWebProcess /tmp/squashfs-root/usr/lib/webkitgtk-6.0/WebKitWebProcess; do
      [ -x "$x" ] && ldd "$x" 2>/dev/null | grep '=> /' | grep -v squashfs-root | awk '{print $1}' >> "$OUT/system-libs.txt"
    done
    sort -u -o "$OUT/system-libs.txt" "$OUT/system-libs.txt"
    glibc=$(ldd --version | head -1 | grep -o '[0-9.]*$')
    [ -z "$missing" ] && ok "todas as bibliotecas resolvidas (glibc $glibc)" || fail "bibliotecas ausentes (glibc $glibc): $missing"
    ldd /tmp/squashfs-root/usr/bin/sessionflow 2>&1 | grep -E 'GLIBC|version' | head -3
    # Sem as ferramentas de sandbox do WebKit: mensagem clara, não crash.
    useradd -m tester 2>/dev/null
    msg=$(cd /tmp && runuser -u tester -- env HOME=/home/tester APPIMAGE_EXTRACT_AND_RUN=1 "$BIN" 2>&1); code=$?
    echo "$msg" | head -3
    [ $code -eq 1 ] && echo "$msg" | grep -q 'bubblewrap e xdg-dbus-proxy' && ok "sem bwrap/xdg-dbus-proxy: sai com instrução (código 1)" || fail "sem bwrap: código $code"
    if command -v apt-get >/dev/null; then
      DEBIAN_FRONTEND=noninteractive apt-get install -y -qq --no-install-recommends bubblewrap xdg-dbus-proxy >> "$OUT/install.log" 2>&1
    else
      dnf install -y -q bubblewrap xdg-dbus-proxy >> "$OUT/install.log" 2>&1
    fi
    ok "instalados bubblewrap e xdg-dbus-proxy (comuns em desktops)" ;;
  tarball)
    apt-get update -qq && DEBIAN_FRONTEND=noninteractive apt-get install -y -qq --no-install-recommends libgtk-4-1 libwebkitgtk-6.0-4 $X_DEB > "$OUT/install.log" 2>&1
    tar -C /opt -xzf /dist/sessionflow-*-linux-amd64.tar.gz && ln -s /opt/sessionflow /usr/local/bin/sessionflow
    BIN=sessionflow ;;
esac

step "integração com o desktop"
if [ "$MODE" = deb ] || [ "$MODE" = rpm ]; then
  for f in /usr/bin/sessionflow /usr/share/applications/com.cesarmenegatti.sessionflow.desktop \
           /usr/share/metainfo/com.cesarmenegatti.sessionflow.metainfo.xml \
           /usr/share/icons/hicolor/256x256/apps/com.cesarmenegatti.sessionflow.png \
           /usr/share/icons/hicolor/scalable/apps/com.cesarmenegatti.sessionflow.svg; do
    [ -e "$f" ] && ok "instalado $f" || fail "ausente $f"
  done
fi

step "usuário comum com históricos (fixtures anonimizados)"
useradd -m tester 2>/dev/null
cp -a /seed/. /home/tester/ && chown -R tester:tester /home/tester
mkdir -p /home/tester/projects/demo-app && chown -R tester:tester /home/tester/projects
as() { runuser -u tester -- env HOME=/home/tester APPIMAGE_EXTRACT_AND_RUN="${APPIMAGE_EXTRACT_AND_RUN:-}" TMPDIR=/tmp "$@"; }

step "--version / --check"
v=$(cd /tmp && as "$BIN" --version 2>&1); echo "$v"
[ "$v" = "SessionFlow $EXPECT_VERSION" ] && ok "--version = $v" || fail "--version: $v"
(cd /tmp && as "$BIN" --check) > "$OUT/check.txt" 2>&1; cat "$OUT/check.txt"
grep -q "Claude Code   ok" "$OUT/check.txt" && grep -q "OpenAI Codex  ok" "$OUT/check.txt" && grep -q "OpenCode      ok" "$OUT/check.txt" \
  && ok "--check detecta os três providers" || fail "--check providers"

step "GUI num X virtual"
cat > /tmp/gui.sh <<GUI
cd /tmp
"$BIN" > /tmp/app.out 2>&1 &
APP=\$!
sleep 25
kill -0 \$APP 2>/dev/null && echo ALIVE || echo DEAD
xwininfo -root -tree | grep -i -E 'sessionflow' | head -3 > /tmp/windows.txt
import -window root /tmp/shot.png
# Segunda instância: deve avisar a primeira e sair sem abrir outra janela.
timeout 20 "$BIN" > /tmp/second.out 2>&1; echo "SECOND_EXIT=\$?"
sleep 2
kill -0 \$APP 2>/dev/null && echo ALIVE_AFTER_SECOND || echo DEAD_AFTER_SECOND
kill \$APP; wait \$APP 2>/dev/null; echo "EXIT=\$?"
GUI
chmod 755 /tmp/gui.sh
as xvfb-run -a -s "-screen 0 1440x900x24" dbus-run-session -- bash /tmp/gui.sh > "$OUT/gui.txt" 2>&1
cat "$OUT/gui.txt"
cp /tmp/app.out "$OUT/app-stderr.txt" 2>/dev/null; cp /tmp/second.out "$OUT/second.txt" 2>/dev/null
cp /tmp/shot.png "$OUT/screenshot.png" 2>/dev/null; cp /tmp/windows.txt "$OUT/windows.txt" 2>/dev/null
LOG=/home/tester/.local/state/sessionflow/sessionflow.log
cp "$LOG" "$OUT/sessionflow.log" 2>/dev/null
grep -q ALIVE "$OUT/gui.txt" && ok "app aberto por 25 s" || fail "app encerrou"
grep -q 'com.cesarmenegatti.sessionflow\|SessionFlow' "$OUT/windows.txt" && ok "janela criada: $(head -1 "$OUT/windows.txt" | tr -s ' ')" || fail "janela não encontrada"
grep -q 'SECOND_EXIT=0' "$OUT/gui.txt" && grep -q ALIVE_AFTER_SECOND "$OUT/gui.txt" && ok "segunda instância saiu (0) e a primeira continuou" || fail "segunda instância"
n=$(grep '"msg":"indexação"' "$LOG" 2>/dev/null | sed -E 's/.*"provider":"([a-z]+)".*/\1/' | sort -u | wc -l)
s=$(grep '"msg":"indexação"' "$LOG" 2>/dev/null | sed -E 's/.*"provider":"([a-z]+)".*"sessions":([0-9]+).*/\1 \2/' | awk '{n[$1]=$2} END{for(p in n) t+=n[p]; print t+0}')
[ "${s:-0}" -ge 3 ] && ok "indexação: $n providers, $s sessões" || fail "indexação ($n, $s)"
if grep -q -E '"level":"ERROR"|panic' "$LOG"; then fail "erros no log"; grep -E '"level":"ERROR"|panic' "$LOG" | head -5; else ok "log sem ERROR/panic"; fi
if grep -q -E 'SIGTRAP|SIGSEGV|^panic|\*\* .*ERROR \*\*|bwrap:' "$OUT/app-stderr.txt" "$OUT/second.txt"; then fail "crash no stderr"; grep -h -m3 -E 'SIGTRAP|SIGSEGV|^panic|ERROR \*\*|bwrap:' "$OUT/app-stderr.txt" "$OUT/second.txt"; else ok "stderr sem crash"; fi
sd=$(convert "$OUT/screenshot.png" -colorspace Gray -format '%[fx:standard_deviation]' info: 2>/dev/null)
# Página carregada = fundo escuro do app com conteúdo. Janela em branco do
# GTK (webview que não carregou) também tem desvio alto, então o critério é
# o pixel da área principal.
bg=$(convert "$OUT/screenshot.png" -format '%[fx:int(255*p{200,700}.r)]' info: 2>/dev/null)
[ "${bg:-255}" -lt 60 ] && awk -v s="${sd:-0}" 'BEGIN{exit !(s>0.03)}' && ok "interface renderizada (fundo do app, desvio $sd)" || fail "janela sem a interface (pixel $bg, desvio $sd)"

step "desinstalação"
case $MODE in
  deb) apt-get remove -y -qq sessionflow > /dev/null 2>&1 ;;
  rpm) dnf remove -y -q sessionflow > /dev/null 2>&1 ;;
  *) rm -f "$BIN" ;;
esac
if [ "$MODE" = deb ] || [ "$MODE" = rpm ]; then
  [ ! -e /usr/bin/sessionflow ] && [ ! -e /usr/share/applications/com.cesarmenegatti.sessionflow.desktop ] && ok "removido" || fail "restos após remoção"
fi
[ -f /home/tester/.local/share/sessionflow/index.db ] && [ -f /home/tester/.claude/projects/-home-tester-projects-demo-app/11111111-1111-4111-8111-111111111111.jsonl ] \
  && ok "dados do usuário e históricos preservados" || fail "dados do usuário"
