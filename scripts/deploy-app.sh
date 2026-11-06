#!/usr/bin/env bash
# Copies a built AppLoad app folder to the tablet. It only writes under
# /home/root/xovi/exthome/appload/<name>; delete that folder to uninstall.
# Usage: scripts/deploy-app.sh dist/ink-sudoku [ssh-host]   (default host: remarkable)
# Then tap Reload in AppLoad on the tablet.
set -euo pipefail
src="${1:?usage: deploy-app.sh <app dir> [ssh-host]}"
host="${2:-remarkable}"
name="$(basename "$src")"
[[ "$name" =~ ^[A-Za-z0-9._-]+$ && "$name" != "." && "$name" != ".." ]] || { echo "bad app name: $name"; exit 1; }
dest="/home/root/xovi/exthome/appload"
ssh "$host" "test -d $dest" || { echo "AppLoad folder $dest missing on $host (is xovi/appload installed?)"; exit 1; }
ssh "$host" "rm -rf $dest/$name"
scp -O -r "$src" "$host:$dest/"   # -O: dropbear has no sftp-server
ssh "$host" "chmod +x $dest/$name/backend/entry 2>/dev/null || true; ls -la $dest/$name"
echo "deployed $name; tap Reload in AppLoad"
