#!/usr/bin/env bash
# Builds dist/ink-sudoku/ (copy to /home/root/xovi/exthome/appload/ on the tablet;
# from the repo root: scripts/deploy-app.sh dist/ink-sudoku).
set -euo pipefail
cd "$(dirname "$0")"
out=../dist/ink-sudoku
rm -rf "$out" && mkdir -p "$out"
[ -f icon.png ] || (pixi run go run mkicon.go icon.png)
if [ -d ../backend/cmd/sudoku ]; then
    mkdir -p "$out/backend"
    (cd ../backend && pixi run sh -c 'CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o ../dist/ink-sudoku/backend/entry ./cmd/sudoku/')
else
    echo "warning: ../backend/cmd/sudoku doesn't exist yet; built without backend/entry (the app will wait for a game)" >&2
fi
../.pixi/envs/default/lib/qt6/rcc --binary -o "$out/resources.rcc" app.qrc
cp manifest.json icon.png "$out/"
echo "built $out"
