#!/usr/bin/env bash
# Pacotes do macOS (runner macos-latest): app universal (Intel + Apple
# Silicon) com assinatura ad-hoc, .dmg e .zip do .app, verificados
# (lipo, codesign, --version, --selftest). Saída em dist/.
set -euo pipefail
cd "$(dirname "$0")/../.."
V=$(tr -d '[:space:]' < VERSION)
APP=bin/SessionFlow.app
mkdir -p dist bin
export MACOSX_DEPLOYMENT_TARGET=13.0

echo "==> binário universal $V"
for arch in arm64 amd64; do
  CGO_ENABLED=1 GOARCH=$arch go build -tags production -trimpath -buildvcs=false \
    -ldflags "-w -s -X main.version=$V" -o "bin/sessionflow-$arch" .
done
lipo -create -output bin/sessionflow bin/sessionflow-arm64 bin/sessionflow-amd64
lipo -info bin/sessionflow | tee /dev/stderr | grep -q "x86_64 arm64\|arm64 x86_64"

echo "==> SessionFlow.app"
rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"
cp bin/sessionflow "$APP/Contents/MacOS/sessionflow"
cp build/darwin/icons.icns "$APP/Contents/Resources/"
[ -f build/darwin/Assets.car ] && cp build/darwin/Assets.car "$APP/Contents/Resources/"
sed -e "s#<string>0.1.0</string>#<string>$V</string>#g" build/darwin/Info.plist > "$APP/Contents/Info.plist"
plutil -lint "$APP/Contents/Info.plist"
xattr -cr "$APP"
codesign --force --deep --sign - "$APP"
codesign --verify --deep --strict "$APP"
"$APP/Contents/MacOS/sessionflow" --version | grep -qx "SessionFlow $V"
"$APP/Contents/MacOS/sessionflow" --selftest | tee /dev/stderr | grep -q "SELFTEST OK"

echo "==> .zip do .app (usado pela atualização) e .dmg"
ditto -c -k --keepParent "$APP" "dist/SessionFlow-$V-macos-universal.zip"
rm -rf dmg && mkdir dmg && cp -R "$APP" dmg/ && ln -s /Applications dmg/Applications
hdiutil create -volname SessionFlow -srcfolder dmg -ov -format UDZO "dist/SessionFlow-$V-macos-universal.dmg"
rm -rf dmg

echo "==> verificação: .zip extraído continua assinado"
rm -rf unz && mkdir unz && ditto -x -k "dist/SessionFlow-$V-macos-universal.zip" unz
codesign --verify --deep --strict unz/SessionFlow.app
rm -rf unz
ls -la dist
