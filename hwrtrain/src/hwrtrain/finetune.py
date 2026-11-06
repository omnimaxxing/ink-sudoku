"""Fine-tune the shipped model on one player's logged corrections.

  python -I -m hwrtrain.finetune corrections.jsonl [more.jsonl ...] \
      [--base ../backend/hwr/model.bin] [--out ../backend/hwr/model.user.bin]

Starts from model.bin itself (dequantised weights; no training checkpoint
needed), so it can run anywhere the logs and this repo are. Each step mixes:
  * the player's samples, re-rasterised through raster.py with light stroke
    augmentation (their ink, many small variations), label 0 -> reject;
  * replay of the base training mix (if hwrtrain/data/cache exists), which
    keeps the model from forgetting other shapes and the reject class.
An L2-SP penalty also pulls weights toward the base model.

Guard rails: 20% of the player's samples (by timestamp hash) are held out;
the new model is written only if held-out accuracy does not drop and, when
the validation cache exists, writer-independent digit accuracy and false
accepts do not regress beyond small margins (override with --force). The
reject threshold is carried over from the base model.

On the tablet, load the result with hwr.LoadModel and use it in place of
DefaultModel.
"""

from __future__ import annotations

import argparse
import multiprocessing as mp
import zlib
from pathlib import Path

import numpy as np
import torch
import torch.nn.functional as F

from . import data, raster, synth
from .export import BACKEND
from .modelbin import REJECT, read_model, write_model
from .samples import read_jsonl
from .train import GROUPS, augment, load_train, load_val, predict


def _render(args):
    strokes, n, seed = args
    rng = np.random.default_rng(seed)
    out = [raster.rasterize(strokes)]
    for _ in range(n - 1):
        out.append(raster.rasterize(synth.augment(strokes, rng, 0.6), synth.random_radius(rng)))
    return np.stack(out)


def render_user(samples, n_aug, seed=0):
    jobs = [(s.strokes, n_aug, seed * 1_000_003 + i) for i, s in enumerate(samples)]
    with mp.Pool() as pool:
        X = pool.map(_render, jobs, chunksize=4)
    y = np.repeat([REJECT if s.label == 0 else s.label for s in samples], n_aug)
    return np.clip(np.round(np.concatenate(X) * 255), 0, 255).astype(np.uint8), y.astype(np.int64)


def decide(p, thr):
    b = p.argmax(1)
    return np.where((b >= 1) & (b <= 9) & (p.max(1) >= thr), b, 0)


def user_accuracy(net, samples, thr, dev):
    if not samples:
        return None
    X = np.stack([raster.rasterize(s.strokes) for s in samples])
    p = predict(net, np.clip(np.round(X * 255), 0, 255).astype(np.uint8), dev)
    d = decide(p, thr)
    lab = np.array([s.label for s in samples])
    return float((d == lab).mean())


def base_metrics(net, thr, dev):
    try:
        val = load_val()
    except FileNotFoundError:
        return None
    out = {}
    for k in ("pendigits", "uji_digits", "mnist_val"):
        X, y = val[k]
        m = (y >= 1) & (y <= 9)
        out[k] = float((decide(predict(net, X[m], dev), thr) == y[m]).mean())
    neg = [val[k][0] for k in ("uji_neg", "scribble", "scratch", "overlay", "pair")]
    out["far"] = float(np.mean(decide(predict(net, np.concatenate(neg), dev), thr) > 0))
    return out


def finetune(paths, base, out, steps=400, lr=3e-4, l2sp=1e-3, n_aug=40, user_frac=0.5, force=False,
             device=None, holdout=0.2, log=print):
    dev = device or torch.device("mps" if torch.backends.mps.is_available() else "cpu")
    samples = [s for p in paths for s in read_jsonl(p)]
    if not samples:
        raise SystemExit("no samples")
    flags = [len(samples) >= 15 and (zlib.crc32(f"{s.ts}:{i}".encode()) % 100) < holdout * 100
             for i, s in enumerate(samples)]
    hold = [s for s, h in zip(samples, flags) if h]
    train_s = [s for s, h in zip(samples, flags) if not h]
    mf = read_model(base)
    thr = mf.threshold
    net = mf.net.to(dev)
    w0 = [p.detach().clone() for p in net.parameters()]
    before_user = user_accuracy(net, hold, thr, dev)
    before_base = base_metrics(net, thr, dev)
    log(f"{len(samples)} samples ({len(train_s)} train, {len(hold)} held out); threshold {thr:.3f}")
    log(f"before: held-out player acc {before_user}  base {before_base}")

    Xu, yu = render_user(train_s, n_aug)
    try:
        G = load_train()
        names = [k for k in GROUPS if k in G]
        Xr = np.concatenate([G[k][0] for k in names])
        yr = np.concatenate([G[k][1] for k in names]).astype(np.int64)
        br = np.concatenate([np.full(len(G[k][0]), GROUPS[k][1]) for k in names])
        wr = np.concatenate([np.full(len(G[k][0]), GROUPS[k][0] / len(G[k][0])) for k in names])
        wr = torch.from_numpy(wr / wr.sum())
    except FileNotFoundError:
        Xr = None
        log("no training cache: fine-tuning on player samples only")

    opt = torch.optim.AdamW(net.parameters(), lr=lr, weight_decay=0)
    bs = 128
    nu = bs if Xr is None else int(bs * user_frac)
    rng = np.random.default_rng(0)
    net.train()
    for step in range(steps):
        iu = rng.integers(0, len(Xu), nu)
        xb, yb, bm = [Xu[iu]], [yu[iu]], [np.zeros(nu, bool)]
        if Xr is not None:
            ir = torch.multinomial(wr, bs - nu, replacement=True).numpy()
            xb.append(Xr[ir]); yb.append(yr[ir]); bm.append(br[ir])
        x = torch.from_numpy(np.concatenate(xb).astype(np.float32) / 255.0)[:, None].to(dev)
        y = torch.from_numpy(np.concatenate(yb)).to(dev)
        x = augment(x, torch.from_numpy(np.concatenate(bm)).to(dev), 0.5)
        loss = F.cross_entropy(net(x), y)
        loss = loss + l2sp * sum(((p - q) ** 2).sum() for p, q in zip(net.parameters(), w0))
        opt.zero_grad(set_to_none=True)
        loss.backward()
        opt.step()
    net.eval()
    blob = write_model(net, None, thr)
    q = read_model(blob).net.to(dev)   # judge the quantised result
    after_user = user_accuracy(q, hold, thr, dev)
    after_base = base_metrics(q, thr, dev)
    log(f"after:  held-out player acc {after_user}  base {after_base}")
    ok = True
    if before_user is not None and after_user < before_user:
        ok = False
        log("held-out player accuracy dropped")
    if before_base and after_base:
        if after_base["far"] > before_base["far"] + 0.002:
            ok = False
            log("false-accept rate regressed")
        for k in ("pendigits", "uji_digits", "mnist_val"):
            if after_base[k] < before_base[k] - 0.01:
                ok = False
                log(f"base accuracy on {k} regressed")
    if ok or force:
        Path(out).write_bytes(blob)
        log(f"wrote {out} ({len(blob)} bytes, crc {read_model(blob).crc:08x})")
    else:
        log("not written (use --force to override)")
    return dict(before_user=before_user, after_user=after_user, before_base=before_base,
                after_base=after_base, written=ok or force)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("jsonl", nargs="+")
    ap.add_argument("--base", default=str(BACKEND / "model.bin"))
    ap.add_argument("--out", default=str(BACKEND / "model.user.bin"))
    ap.add_argument("--steps", type=int, default=400)
    ap.add_argument("--lr", type=float, default=3e-4)
    ap.add_argument("--force", action="store_true")
    a = ap.parse_args()
    finetune(a.jsonl, a.base, a.out, a.steps, a.lr, force=a.force)


if __name__ == "__main__":
    main()
