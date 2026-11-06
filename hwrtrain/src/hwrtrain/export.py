"""Export a checkpoint to backend/hwr/model.bin and write the Go/PyTorch
parity file backend/hwr/testdata/parity.bin.gz.

  python -I -m hwrtrain.export runs/A/best.pt

Parity strategy
  * The reference network is read_model(model.bin): the dequantised int8
    weights with BN folded -- exactly what Go executes -- not the float
    training checkpoint.
  * Stroke samples (held-out writers: pendigits-orig.tes, UJI2 tst digits
    and letters, plus synthetic scribbles) are rescaled to tablet-sized
    float32 screen coordinates, rasterised by raster.py and scored. Go
    re-rasterises the same strokes with raster.go and runs model.go; the
    test compares top-1 (and, for the first NRASTER samples, the raster
    itself, so rasteriser drift and inference drift are told apart).
  * Bitmap samples (MNIST test) check the network alone.

parity.bin.gz layout (little-endian, gzip-compressed):
  magic "HWRP", u32 version=1, u32 nstroke, u32 nbitmap, u32 nraster, u32 nclass
  nstroke x { u8 label, u16 nstrokes, nstrokes x { u16 npts, npts x (f32 x, f32 y) } }
  nbitmap x { u8 label, [784]u8 }
  (nstroke+nbitmap) x [nclass]f32 reference logits
  nraster x [784]f32 reference rasters (first nraster stroke samples)
"""

from __future__ import annotations

import argparse
import gzip
import struct
from pathlib import Path

import numpy as np
import torch

from . import data, raster, synth
from .evaluate import calibrate, load_net
from .modelbin import read_model, write_model

BACKEND = Path(__file__).resolve().parents[3] / "backend" / "hwr"


def tabletize(strokes, rng):
    """Rescale a sample to a pen-on-tablet size and position (float32 px)."""
    pts = np.concatenate(strokes)
    lo, hi = pts.min(0), pts.max(0)
    h = max(float(hi[1] - lo[1]), float(hi[0] - lo[0]), 1e-6)
    target = rng.uniform(25, 110)
    s = target / h
    off = np.array([rng.uniform(100, 1300), rng.uniform(100, 1700)], np.float32)
    return [((x - lo) * s + off).astype(np.float32) for x in strokes]


def parity_samples(rng, n_stroke=1500, n_bitmap=500):
    pd = data.pendigits("tes")
    uji = [s for s in data.uji2() if s.writer.startswith("tst")]
    dig = [s for s in uji if s.label.isdigit()]
    let = [s for s in uji if not s.label.isdigit()]
    pick = lambda xs, n: [xs[i] for i in rng.choice(len(xs), min(n, len(xs)), replace=False)]
    items = [(s.strokes, int(s.label)) for s in pick(pd, 800)]
    items += [(s.strokes, int(s.label)) for s in pick(dig, 300)]
    items += [(s.strokes, 10) for s in pick(let, 250)]
    while len(items) < n_stroke:
        items.append(([x * 50 for x in synth.scribble(rng)], 10))
    items = [(tabletize(st, rng), y) for st, y in items[:n_stroke]]
    xm, ym = data.mnist("test")
    bi = rng.choice(len(xm), n_bitmap, replace=False)
    return items, [(xm[i], int(ym[i])) for i in bi]


def write_parity(model_path, out, nraster=300, seed=7):
    rng = np.random.default_rng(seed)
    mf = read_model(model_path)
    strokes, bitmaps = parity_samples(rng)
    rasters = np.stack([raster.rasterize(st) for st, _ in strokes]).astype(np.float32)
    imgs = np.concatenate([rasters, np.stack([b / np.float32(255) for b, _ in bitmaps]).astype(np.float32)])
    with torch.no_grad():
        logits = mf.net(torch.from_numpy(imgs)[:, None]).numpy().astype("<f4")
    buf = bytearray(b"HWRP")
    buf += struct.pack("<IIIII", 1, len(strokes), len(bitmaps), nraster, logits.shape[1])
    for st, y in strokes:
        buf += struct.pack("<BH", y, len(st))
        for s in st:
            buf += struct.pack("<H", len(s)) + np.ascontiguousarray(s[:, :2], "<f4").tobytes()
    for b, y in bitmaps:
        buf += struct.pack("<B", y) + b.astype(np.uint8).tobytes()
    buf += logits.tobytes()
    buf += rasters[:nraster].astype("<f4").tobytes()
    out.parent.mkdir(parents=True, exist_ok=True)
    with gzip.open(out, "wb", compresslevel=9) as f:
        f.write(bytes(buf))
    return len(strokes), len(bitmaps)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("checkpoint")
    ap.add_argument("--out", default=str(BACKEND / "model.bin"))
    ap.add_argument("--parity", default=str(BACKEND / "testdata" / "parity.bin.gz"))
    ap.add_argument("--target-far", type=float, default=0.002)
    ap.add_argument("--device", default="mps" if torch.backends.mps.is_available() else "cpu")
    a = ap.parse_args()
    dev = torch.device(a.device)
    net, _ = load_net(a.checkpoint)
    blob = write_model(net, None, 0.5)
    # calibrate on the *quantised* network: that is what ships
    q = read_model(blob).net.to(dev)
    thr, rows = calibrate(q, dev, a.target_far)
    for r in rows:
        print({k: round(v, 4) for k, v in r.items()})
    blob = write_model(net, a.out, thr)
    print(f"wrote {a.out}: {len(blob)} bytes, threshold {thr}, crc {read_model(blob).crc:08x}")
    ns, nb = write_parity(a.out, Path(a.parity))
    print(f"wrote {a.parity}: {ns} stroke + {nb} bitmap samples")


if __name__ == "__main__":
    main()
