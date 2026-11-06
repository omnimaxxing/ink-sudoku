# Ink Sudoku (reMarkable Paper Pure)

Pen-only Sudoku as an AppLoad QML app with a static Go backend. Plan, bar and status: `../PLAN.md`.

```
backend/            Go module `sudoku` (stdlib only, static linux/arm64)
  appload/          AppLoad backend socket protocol
  pen/              stylus evdev reader (parallel to xochitl, no grab)
  engine/           generator, solver, human-technique grader, hints
  hwr/              stroke rasterizer, int8 digit CNN, cell ink analysis, correction log
  cmd/sudokugen/    puzzle generator CLI (tuning stats)
  cmd/probe/        G1 probe backend
app/                Ink Sudoku QML frontend + manifest/icon; build.sh -> dist/ink-sudoku
hwrtrain/           Python (uv) training + fine-tune pipeline for hwr/model.bin — see its README
probe/              G1 probe app (QML): ink latency per display method, pen path comparison
dev/                Mac QML stubs for net.asivery.* + offscreen screenshot harness
tools/mkicon.go     app icon
```

Commands (from this folder):
```sh
cd backend && pixi run go test ./...                     # whole suite, ~1 s
cd backend && pixi run go run ./cmd/sudokugen -level all -n 200 -q
./probe/build.sh && ../scripts/deploy-app.sh dist/probe   # then Reload in AppLoad
./app/build.sh && ../scripts/deploy-app.sh dist/ink-sudoku
QT_QPA_PLATFORM=offscreen .pixi/envs/default/bin/qml6 -I dev/stubs dev/shot.qml -- "$PWD/probe/ui/main.qml" /tmp/shot.png
QT_QPA_PLATFORM=offscreen .pixi/envs/default/bin/qml6 -I dev/stubs dev/shot.qml -- [--hint] "$PWD/app/ui/main.qml" /tmp/inksudoku.png
# more screens: --chooser --teach --teach-finish --ink --ink-many N; stub: --daily --solved --first-run (see dev/shot.qml header)
# against the real backend: record dev/fixtures/session.json, then replay it through the app (steps 2..N)
cd backend && INK_FIXTURES=$PWD/../dev/fixtures pixi run go test ./cmd/sudoku -run Fixtures
QT_QPA_PLATFORM=offscreen .pixi/envs/default/bin/qml6 -I dev/stubs dev/shot.qml -- --replay [N] "$PWD/app/ui/main.qml" /tmp/replay.png
```

Licensing: our code is written from protocol facts, not copied from GPL repos (debemtech/remarkable-games,
asivery/*), so the project can stay MIT. remarkable-sudoku (MIT) is reference only so far.
