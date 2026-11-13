#!/bin/sh
# Builds SteamIdler into dist/: macOS (universal .app in a zip), Windows (.exe) and Linux (tar.gz),
# each for amd64 and arm64. MAC_ONLY=1 stops after macOS.
# Needs Go 1.22+ and Python 3. Works on macOS, Linux and Windows (Git Bash).
set -e
cd "$(dirname "$0")/.."
VERSION=${VERSION:-5.0.0}
LDFLAGS="-s -w -X steamidler/internal/hub.Version=$VERSION"
rm -rf dist && mkdir -p dist

python3 scripts/i18n.py >/dev/null
python3 scripts/winres.py ui/icon.ico cmd/steamidler >/dev/null

echo "macOS..."
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags="$LDFLAGS" -o dist/steamidler-arm64 ./cmd/steamidler
CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags="$LDFLAGS" -o dist/steamidler-amd64 ./cmd/steamidler
python3 scripts/makefat.py dist/steamidler dist/steamidler-amd64 dist/steamidler-arm64
APP="dist/SteamIdler.app"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"
cp dist/steamidler "$APP/Contents/MacOS/steamidler" && chmod 755 "$APP/Contents/MacOS/steamidler"
sed "s/5.0.0/$VERSION/" packaging/Info.plist > "$APP/Contents/Info.plist"
cp packaging/AppIcon.icns "$APP/Contents/Resources/AppIcon.icns"
if command -v codesign >/dev/null 2>&1; then codesign --force --deep -s - "$APP"; fi
rm dist/steamidler dist/steamidler-arm64 dist/steamidler-amd64
cp "packaging/Установка.txt" dist/ && (cd dist && zip -qry "SteamIdler-$VERSION-macOS.zip" SteamIdler.app "Установка.txt") && rm "dist/Установка.txt"

[ -n "$MAC_ONLY" ] && { echo "Done: dist/"; exit 0; }
echo "Windows..."
for arch in amd64 arm64; do
  CGO_ENABLED=0 GOOS=windows GOARCH=$arch go build -trimpath -ldflags="$LDFLAGS -H windowsgui" -o "dist/SteamIdler-$VERSION-windows-$arch.exe" ./cmd/steamidler
done
echo "Linux..."
for arch in amd64 arm64; do
  CGO_ENABLED=0 GOOS=linux GOARCH=$arch go build -trimpath -ldflags="$LDFLAGS" -o dist/steamidler ./cmd/steamidler
  tar -C dist -czf "dist/SteamIdler-$VERSION-linux-$arch.tar.gz" steamidler && rm dist/steamidler
done
echo "Done: dist/"
