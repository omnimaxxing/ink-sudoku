#!/usr/bin/env bash
# Builds dist/probe/ (copy to /home/root/xovi/exthome/appload/ on the tablet).
set -euo pipefail
cd "$(dirname "$0")"
out=../dist/probe
rm -rf "$out" && mkdir -p "$out/backend"
pixi run sh -c 'cd ../backend && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o ../dist/probe/backend/entry ./cmd/probe/'
../.pixi/envs/default/lib/qt6/rcc --binary -o "$out/resources.rcc" probe.qrc
cp manifest.json icon.png "$out/"
echo "built $out"
