#!/usr/bin/env bash
# Pacotes do Windows (runner windows-latest, bash do Git): instalador NSIS
# por usuário e .zip portátil, com o binário verificado (--version e
# --selftest) instalado e extraído. Saída em dist/.
set -euo pipefail
cd "$(dirname "$0")/../.."
# expect PADRÃO CMD...: roda, mostra a saída e exige PADRÃO (o Git Bash do runner não tem o stderr como arquivo para o tee)
expect() { local pat=$1; shift; "$@" > out.txt; cat out.txt; grep -q -- "$pat" out.txt; rm -f out.txt; }
VERSION=$(tr -d '[:space:]' < VERSION)
V=$VERSION   # rc só muda a tag e o release, não os arquivos
mkdir -p dist bin

echo "==> recursos do executável (ícone, manifesto, versão $VERSION)"
node -e "const f='build/windows/info.json',j=require('./'+f);j.fixed.file_version=j.fixed.product_version=process.argv[1];for(const k in j.info){j.info[k].FileVersion=j.info[k].ProductVersion=process.argv[1]}require('fs').writeFileSync(f,JSON.stringify(j,null,2))" "$VERSION"
wails3 generate syso -arch amd64 -icon build/windows/icon.ico -manifest build/windows/wails.exe.manifest -info build/windows/info.json -out wails_windows_amd64.syso

echo "==> binário"
go build -tags production -trimpath -buildvcs=false -ldflags "-w -s -H windowsgui -X main.version=$V" -o bin/sessionflow.exe .
rm -f wails_windows_amd64.syso
expect "^SessionFlow $V\$" ./bin/sessionflow.exe --version

echo "==> instalador (NSIS, por usuário)"
command -v makensis >/dev/null || choco install nsis -y --no-progress
export PATH="$PATH:/c/Program Files (x86)/NSIS"
wails3 generate webview2bootstrapper -dir build/windows/nsis
(cd build/windows/nsis && makensis -DINFO_PRODUCTVERSION="$VERSION" -DARG_WAILS_AMD64_BINARY="$(cygpath -w "$PWD/../../../bin/sessionflow.exe")" project.nsi)
mv bin/sessionflow-amd64-installer.exe "dist/SessionFlow-$V-windows-amd64-setup.exe"

echo "==> portátil (.zip)"
rm -rf stage-win && mkdir stage-win && cp bin/sessionflow.exe LICENSE stage-win/
(cd stage-win && 7z a -tzip "../dist/SessionFlow-$V-windows-amd64-portable.zip" sessionflow.exe LICENSE >/dev/null)
rm -rf stage-win

echo "==> verificação: instalar em silêncio, rodar, desinstalar"
inst="$LOCALAPPDATA/Programs/SessionFlow"
"dist/SessionFlow-$V-windows-amd64-setup.exe" /S
test -f "$inst/sessionflow.exe"
expect "^SessionFlow $V\$" "$inst/sessionflow.exe" --version
expect "SELFTEST OK" "$inst/sessionflow.exe" --selftest
reg query 'HKCU\Software\Microsoft\Windows\CurrentVersion\Uninstall\SessionFlow' >/dev/null
"$inst/uninstall.exe" /S
for _ in $(seq 1 30); do [ -e "$inst/sessionflow.exe" ] || break; sleep 1; done # o desinstalador sai antes de terminar
test ! -e "$inst/sessionflow.exe"
! reg query 'HKCU\Software\Microsoft\Windows\CurrentVersion\Uninstall\SessionFlow' >/dev/null 2>&1

echo "==> verificação: portátil"
rm -rf port && mkdir port && (cd port && 7z x "../dist/SessionFlow-$V-windows-amd64-portable.zip" >/dev/null)
expect "SELFTEST OK" ./port/sessionflow.exe --selftest
rm -rf port
ls -la dist
