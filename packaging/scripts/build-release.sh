#!/usr/bin/env bash
# Build de release dentro do contêiner de packaging/docker/Dockerfile.build.
# Entrada: código em /src (somente leitura). Saída: /out com o binário,
# AppImage, .deb, .rpm, tar.gz e SHA256SUMS.
set -euo pipefail

VERSION=$(tr -d '[:space:]' < /src/VERSION)
APP_ID=com.cesarmenegatti.sessionflow
WORK=/tmp/build
OUT=/out

# Cópia de trabalho: o contêiner nunca escreve na árvore do usuário.
rm -rf "$WORK" && mkdir -p "$WORK"
tar -C /src --exclude=./frontend/node_modules --exclude=./frontend/dist --exclude=./frontend/bindings --exclude=./bin --exclude=./dist \
    -cf - . | tar -C "$WORK" -xf -
cd "$WORK"

echo "==> bindings"
# Gerados a partir dos serviços Go (não versionados).
wails3 generate bindings -f '-tags production' -clean=true -ts -i

echo "==> frontend"
(cd frontend && npm ci --no-audit --no-fund && npm run build)

echo "==> testes"
go test ./...

echo "==> binário $VERSION"
CGO_ENABLED=1 go build -tags production -trimpath -ldflags "-s -w -X main.version=$VERSION" -o stage/sessionflow .
./stage/sessionflow --version | grep -qx "SessionFlow $VERSION"

echo "==> ícones e metadados"
for size in 16 24 32 48 64 128 256 512; do
  mkdir -p "stage/icons/${size}x${size}/apps"
  rsvg-convert -w "$size" -h "$size" packaging/icons/sessionflow.svg -o "stage/icons/${size}x${size}/apps/$APP_ID.png"
done
mkdir -p stage/icons/scalable/apps
cp packaging/icons/sessionflow.svg "stage/icons/scalable/apps/$APP_ID.svg"
cp packaging/linux/$APP_ID.desktop packaging/linux/$APP_ID.metainfo.xml LICENSE stage/
desktop-file-validate stage/$APP_ID.desktop
appstreamcli validate --no-net --explain stage/$APP_ID.metainfo.xml

mkdir -p dist
echo "==> .deb e .rpm"
export VERSION
for fmt in deb rpm; do
  wails3 tool package -name sessionflow -format "$fmt" -config packaging/nfpm.yaml -out dist
done

echo "==> AppImage"
mkdir -p build/appimage
cp stage/sessionflow build/appimage/
cp "stage/icons/256x256/apps/$APP_ID.png" "build/appimage/$APP_ID.png"
cp "stage/$APP_ID.desktop" build/appimage/
(cd build/appimage && APPIMAGE_EXTRACT_AND_RUN=1 wails3 generate appimage -binary sessionflow \
    -icon "$APP_ID.png" -desktopfile "$APP_ID.desktop" -outputdir "$WORK/dist" -builddir "$WORK/build/appimage/work")
# AppStream dentro da AppImage (lido por lojas e integradores).
for f in dist/*.AppImage; do mv "$f" "dist/SessionFlow-$VERSION-x86_64.AppImage"; done

echo "==> binário avulso"
tar -C stage -czf "dist/sessionflow-$VERSION-linux-amd64.tar.gz" sessionflow $APP_ID.desktop $APP_ID.metainfo.xml icons LICENSE

for f in dist/*.deb; do mv "$f" "dist/sessionflow_${VERSION}_amd64.deb"; done
for f in dist/*.rpm; do mv "$f" "dist/sessionflow-${VERSION}-1.x86_64.rpm"; done
(cd dist && sha256sum -- * > SHA256SUMS)

mkdir -p "$OUT"
cp -r dist/. "$OUT/"
mkdir -p "$OUT/stage" && cp -r stage/. "$OUT/stage/"
chown -R "${HOST_UID:-0}:${HOST_GID:-0}" "$OUT"
echo "==> pronto"; ls -la "$OUT"
