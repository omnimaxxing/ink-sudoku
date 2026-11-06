#!/usr/bin/env bash
# Builds dist/ink-kakuro/ (copy to /home/root/xovi/exthome/appload/ on the tablet;
# from the repo root: scripts/deploy-app.sh dist/ink-kakuro).
set -euo pipefail
cd "$(dirname "$0")"
out=../dist/ink-kakuro
rm -rf "$out" && mkdir -p "$out"
[ -f icon.png ] || (pixi run go run mkicon.go icon.png)
if [ -d ../backend/cmd/kakuro ]; then
    mkdir -p "$out/backend"
    if ! (cd ../backend && pixi run sh -c 'CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o ../dist/ink-kakuro/backend/entry ./cmd/kakuro/'); then
        rm -rf "$out/backend"
        echo "warning: ../backend/cmd/kakuro didn't build; built without backend/entry (the app will wait for a game)" >&2
    fi
else
    echo "warning: ../backend/cmd/kakuro doesn't exist yet; built without backend/entry (the app will wait for a game)" >&2
fi
../.pixi/envs/default/lib/qt6/rcc --binary -o "$out/resources.rcc" app.qrc
cp manifest.json icon.png "$out/"
echo "built $out"
