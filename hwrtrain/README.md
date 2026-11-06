# hwrtrain — training pipeline for `backend/hwr`

Trains the on-tablet digit/reject CNN, exports `backend/hwr/model.bin` and
the Go/PyTorch parity file, and fine-tunes on a player's correction log.

```sh
cd hwrtrain
/opt/homebrew/bin/uv sync                      # .venv, Python 3.12, torch (MPS)
# datasets -> data/<name>/ (gitignored), see "Data" below
.venv/bin/python -I -m hwrtrain.build          # pre-render caches -> data/cache/ (~6 min)
# the shipped model.bin (crc 79e37457) is this run:
.venv/bin/python -I -m hwrtrain.train --arch c16-p-c32-p-c64-p-f128 --epochs 36 \
    --reject-boost 1.3 --seed 1 --out runs/A3                        # ~7 min on MPS
.venv/bin/python -I -m hwrtrain.export runs/A3/best.pt   # default --target-far 0.002 -> threshold 0.97
.venv/bin/python -I -m hwrtrain.evaluate ../backend/hwr/model.bin
.venv/bin/python -I -m hwrtrain.simulate ../backend/hwr/model.bin  # fine-tune loop on unseen writers
cd ../backend && pixi run go test ./hwr/...
# report-only Go evaluations (skipped by default):
.venv/bin/python -I -m hwrtrain.calib          # (from hwrtrain/) exports strokes for these
HWR_CALIB=$PWD/../hwrtrain/data/cache/calib_strokes.jsonl pixi run go test ./hwr/ -run ScratchCalibration -v
HWR_E2E=$PWD/../hwrtrain/data/cache/e2e_strokes.jsonl pixi run go test ./hwr/ -run EndToEnd -v
# on-device personalization (hwr.Personal) on unseen writers; HWR_SPLIT=dev to tune
HWR_PERSONAL=$PWD/../hwrtrain/data/cache/personal_strokes.jsonl pixi run go test ./hwr/ -run PersonalSimulation -v
```

Use MPS (default). On this pre-release macOS the torch CPU backward pass is
pathologically slow (~45 s/step).

## Data (all under `data/`, never committed)

| dir | source | used for | license |
|---|---|---|---|
| `mnist/` | MNIST idx files, ossci-datasets S3 mirror of LeCun's | digits (bitmap + re-drawn as strokes), MNIST test | no explicit license on LeCun's page; commonly redistributed as CC BY-SA 3.0; derived from NIST SD-1/SD-3 |
| `emnist/` | EMNIST `gzip.zip`, biometrics.nist.gov | letters (byclass) as reject negatives | NIST SD-19 derivative (US-government work, freely usable); cite Cohen et al. 2017 |
| `pendigits/` | UCI #81 `pendigits-orig.{tra,tes}` (UNIPEN strokes), via web.archive.org (UCI was down) | pen digits: 30 train / 14 test writers | CC BY 4.0 (UCI) |
| `uji2/` | UCI #177 `ujipenchars2.txt`, via web.archive.org | pen digits + letters/symbols, 40 trn / 20 tst writers | UCI; v1 (#160, a subset of v2) is CC BY 4.0 — v2 page not reachable to confirm |

UJI v1 is a strict subset of v2 and is not used separately. EMNIST *digits*
are deliberately not trained on: their source writers overlap MNIST test.

## Train/inference parity

`src/hwrtrain/raster.py` and `backend/hwr/raster.go` implement one
rasteriser spec: fit the centre-line bbox + stroke footprint into 20x20
(aspect kept), draw anti-aliased capsules of fixed output radius (1.1 px, edge
ramp 0.5 px; calibrated to MNIST's median stroke width), max-combine, then
shift sub-pixel so the centre of mass lands at MNIST's (pixel 14). All
stroke-sourced images — pendigits, UJI, synthetic negatives, and MNIST/EMNIST
redrawn from their skeletons (`build.skeleton_strokes`) — go through
`raster.py`. `export.py` writes held-out strokes plus reference rasters and
logits (from the dequantised `model.bin`, i.e. exactly what Go runs) to
`backend/hwr/testdata/parity.bin.gz`; `TestParity` re-rasterises in Go,
re-runs inference and requires >= 99.9% top-1 agreement and < 1e-3 raster
drift. Change one side only and that test fails.

## Self-improvement

The app logs one JSONL line per corrected misread (`hwr.NewSample` +
`hwr.AppendSample`; format in `backend/hwr/sample.go`).

On the tablet, `hwr.Personal` uses these samples (plus a calibration page)
without any retraining: nearest-sample evidence over `Model.Embed` is blended
into the CNN output before the accept rule (see `backend/hwr/personal.go`).
Its constants were tuned on the "dev" writers of `personal_strokes.jsonl`
(pendigits writers 26-30 and the UJI2 validation writers, never trained on)
and are reported on the "test" writers (pendigits-orig.tes, UJI2 tst) by
`TestPersonalSimulation`.

The heavier option is fine-tuning the network on a Mac:

```sh
.venv/bin/python -I -m hwrtrain.finetune corrections.jsonl --out ../backend/hwr/model.user.bin
```

It starts from `model.bin` (no checkpoint needed), mixes the player's
re-rasterised ink (augmented) with replay of the base training mix, adds an
L2-SP pull toward the base weights, and writes the result only if the
player's held-out samples and the writer-independent validation sets don't
regress. The output (`model.user.bin` by default) is NOT embedded: load it on
the tablet with `hwr.LoadModel` and use it in place of `hwr.DefaultModel()`.

## Layout

```
src/hwrtrain/
  raster.py    reference rasteriser (mirror of raster.go)
  data.py      dataset parsers (idx, UNIPEN, UJI)
  synth.py     stroke augmentation + synthetic negatives
  build.py     writer-disjoint caches, skeleton re-drawing
  modelbin.py  CNN spec, HWR1 writer/reader
  train.py     training (group-weighted batches, on-device augmentation)
  evaluate.py  metrics + reject-threshold calibration
  export.py    model.bin + parity file
  calib.py     stroke exports for the Go-side report tests (calibration, e2e, personal)
  samples.py   correction-log JSONL I/O
  finetune.py  per-player fine-tuning
runs/          checkpoints/logs (gitignored)
```
