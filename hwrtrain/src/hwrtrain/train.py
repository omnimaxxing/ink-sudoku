"""Train the digit/reject CNN.

  python -I -m hwrtrain.train --arch c16-p-c32-p-c64-p-f128 --epochs 36 --reject-boost 1.3 --seed 1 --out runs/A3

Batches mix sources by fixed weights (see GROUPS) so the reject class is
~1/3 of every batch regardless of how many negatives exist. Online
augmentation runs on-device: small affine + elastic for everything, plus
thickness / blur / contrast jitter for scanned bitmaps (MNIST, EMNIST) so
that "looks scanned" is not a class cue.
"""

from __future__ import annotations

import argparse
import json
import time
from pathlib import Path

import numpy as np
import torch
import torch.nn.functional as F

from . import data
from .modelbin import REJECT, make_net

# group -> (sampling weight, is_bitmap)
GROUPS = {
    "mnist": (0.14, True),          # MNIST train bitmaps
    "mnist_pen": (0.16, False),     # MNIST train re-drawn as pen strokes
    "pen": (0.34, False),           # pendigits + UJI2 strokes (augmented)
    "uji_neg": (0.07, False),       # UJI2 letters/symbols (no lookalikes)
    "emnist_pen": (0.09, False),    # EMNIST letters re-drawn as strokes
    "emnist_letters": (0.04, True), # EMNIST letters bitmaps
    "scribble": (0.07, False),
    "scratch": (0.03, False),
    "overlay": (0.03, False),
    "pair": (0.03, False),
}


def load_train(extra: dict | None = None):
    """Return dict group -> (uint8 X (N,28,28), int64 y)."""
    xm, ym = data.mnist("train")
    tp = np.load(data.CACHE / "train_pen.npz")
    em = np.load(data.CACHE / "emnist_letters.npz")
    sk = np.load(data.CACHE / "skel.npz")
    g = {
        "mnist": (xm[:55000], ym[:55000]),
        "mnist_pen": (sk["mnist_train"], sk["mnist_train_y"].astype(np.int64)),
        "emnist_pen": (sk["letters_train"], np.full(len(sk["letters_train"]), REJECT)),
        "pen": (tp["X"], tp["y"].astype(np.int64)),
        "emnist_letters": (em["X_train"], np.full(len(em["X_train"]), REJECT)),
    }
    for k in ("uji", "scribble", "scratch", "overlay", "pair"):
        X = tp[f"Xneg_{k}"]
        g["uji_neg" if k == "uji" else k] = (X, np.full(len(X), REJECT))
    if extra:
        g.update(extra)
    return g


def load_val():
    """Clean (unaugmented) validation sets: dict name -> (X uint8, y)."""
    xm, ym = data.mnist("train")
    v = np.load(data.CACHE / "val_pen.npz")
    out = {"mnist_val": (xm[55000:], ym[55000:])}
    for k in ("pendigits", "uji_digits", "uji_neg", "uji_look"):
        out[k] = (v[f"X_{k}"], v[f"y_{k}"].astype(np.int64))
    for k in ("scribble", "scratch", "overlay", "pair"):
        out[k] = (v[f"X_{k}"], np.full(len(v[f"X_{k}"]), REJECT))
    return out


def augment(x: torch.Tensor, bitmap: torch.Tensor, strength: float = 1.0) -> torch.Tensor:
    """x: (B,1,28,28) float in [0,1]; bitmap: (B,) bool."""
    B, dev = x.shape[0], x.device
    ang = (torch.rand(B, device=dev) * 2 - 1) * 0.10 * strength
    sc = 1 + (torch.rand(B, device=dev) * 2 - 1) * 0.08 * strength
    sh = (torch.rand(B, device=dev) * 2 - 1) * 0.10 * strength
    tx = (torch.rand(B, 2, device=dev) * 2 - 1) * (1.0 / 14) * strength
    cos, sin = torch.cos(ang), torch.sin(ang)
    theta = torch.zeros(B, 2, 3, device=dev)
    theta[:, 0, 0] = cos / sc
    theta[:, 0, 1] = (-sin + sh) / sc
    theta[:, 1, 0] = sin / sc
    theta[:, 1, 1] = cos / sc
    theta[:, :, 2] = tx
    grid = F.affine_grid(theta, x.shape, align_corners=False)
    # elastic: smooth random displacement, stronger for bitmaps
    amp = torch.where(bitmap, 1.3, 0.6).to(dev) * strength * torch.rand(B, device=dev)
    noise = torch.rand(B, 2, 5, 5, device=dev) * 2 - 1
    disp = F.interpolate(noise, size=(28, 28), mode="bicubic", align_corners=False)
    grid = grid + disp.permute(0, 2, 3, 1) * (amp[:, None, None, None] * 2 / 28)
    x = F.grid_sample(x, grid, mode="bilinear", padding_mode="zeros", align_corners=False)

    bm = bitmap[:, None, None, None].float()
    r = torch.rand(B, 1, 1, 1, device=dev)
    t = torch.rand(B, 1, 1, 1, device=dev) * 0.6
    dil = F.max_pool2d(x, 3, 1, 1)
    ero = -F.max_pool2d(-x, 3, 1, 1)
    thick = torch.where(r < 0.3, x + t * (dil - x), torch.where(r < 0.5, x + t * (ero - x), x))
    x = bm * thick + (1 - bm) * x
    k = torch.tensor([[1, 2, 1], [2, 4, 2], [1, 2, 1]], dtype=x.dtype, device=dev).view(1, 1, 3, 3) / 16
    blur = F.conv2d(x, k, padding=1)
    b = (torch.rand(B, 1, 1, 1, device=dev) < 0.3).float() * torch.rand(B, 1, 1, 1, device=dev) * bm
    x = x + b * (blur - x)
    lo = torch.rand(B, 1, 1, 1, device=dev) * 0.15
    hi = 1 - torch.rand(B, 1, 1, 1, device=dev) * 0.25
    cm = (torch.rand(B, 1, 1, 1, device=dev) < 0.3).float() * bm
    x = cm * ((x - lo) / (hi - lo)) + (1 - cm) * x
    return x.clamp(0, 1)


@torch.no_grad()
def predict(net, X: np.ndarray, device, bs=2048) -> np.ndarray:
    net.eval()
    out = []
    for i in range(0, len(X), bs):
        xb = torch.from_numpy(np.asarray(X[i:i + bs], np.float32) / 255.0).to(device)[:, None]
        out.append(F.softmax(net(xb), 1).cpu().numpy())
    return np.concatenate(out) if out else np.zeros((0, 11), np.float32)


def summarize(probs: dict, labels: dict, thr: float = 0.9):
    """Digit sets: forced 1-9 accuracy; reject sets: accept rate at thr."""
    res = {}
    for k, p in probs.items():
        y = labels[k]
        if (y == REJECT).all():
            best = p.argmax(1)
            acc = (best >= 1) & (best <= 9) & (p.max(1) >= thr)
            res[k] = {"far": float(acc.mean())}
        else:
            m = (y >= 1) & (y <= 9)
            pd = p[m][:, 1:10].argmax(1) + 1
            res[k] = {"acc19": float((pd == y[m]).mean())}
    return res


def train(arch, epochs, out, device, batch=256, steps_per_epoch=1200, lr=2.5e-3, seed=0,
          groups=GROUPS, init=None, extra=None, log=None, label_smoothing=0.0):
    log = log or (lambda m: print(m, flush=True))
    torch.manual_seed(seed)
    np.random.seed(seed)
    out = Path(out)
    out.mkdir(parents=True, exist_ok=True)
    G = load_train(extra)
    names = [k for k in groups if k in G]
    X = np.concatenate([G[k][0] for k in names])
    Y = np.concatenate([G[k][1] for k in names]).astype(np.int64)
    isbm = np.concatenate([np.full(len(G[k][0]), groups[k][1]) for k in names])
    w = np.concatenate([np.full(len(G[k][0]), groups[k][0] / len(G[k][0])) for k in names])
    log("train sizes " + ", ".join(f"{k}={len(G[k][0])}" for k in names))
    Xt = torch.from_numpy(X)
    Yt = torch.from_numpy(Y)
    Bt = torch.from_numpy(isbm)
    Wt = torch.from_numpy(w / w.sum())
    val = load_val()

    net = init if init is not None else make_net(arch)
    net = net.to(device)
    opt = torch.optim.AdamW(net.parameters(), lr=lr, weight_decay=1e-4)
    sched = torch.optim.lr_scheduler.OneCycleLR(opt, max_lr=lr, total_steps=epochs * steps_per_epoch, pct_start=0.15)
    best, hist = -1.0, []
    for ep in range(epochs):
        net.train()
        t0 = time.time()
        idx = torch.multinomial(Wt, steps_per_epoch * batch, replacement=True)
        tot = 0.0
        for s in range(steps_per_epoch):
            bi = idx[s * batch:(s + 1) * batch]
            xb = (Xt[bi].float() / 255.0)[:, None].to(device)
            yb = Yt[bi].to(device)
            xb = augment(xb, Bt[bi].to(device))
            loss = F.cross_entropy(net(xb), yb, label_smoothing=label_smoothing)
            opt.zero_grad(set_to_none=True)
            loss.backward()
            opt.step()
            sched.step()
            tot += loss.item()
        probs = {k: predict(net, v[0], device) for k, v in val.items()}
        r = summarize(probs, {k: v[1] for k, v in val.items()})
        pen = (r["pendigits"]["acc19"] * len(val["pendigits"][0]) + r["uji_digits"]["acc19"] * len(val["uji_digits"][0])) / (
            len(val["pendigits"][0]) + len(val["uji_digits"][0]))
        far = np.mean([r[k]["far"] for k in ("uji_neg", "scribble", "scratch", "overlay", "pair")])
        score = pen + r["mnist_val"]["acc19"] - far
        hist.append(dict(epoch=ep, loss=tot / steps_per_epoch, **{k: v for k, v in r.items()}))
        log(f"ep {ep:2d} loss {tot / steps_per_epoch:.4f} {time.time() - t0:5.1f}s  mnist {r['mnist_val']['acc19']:.4f} "
            f"pen {pen:.4f} (pd {r['pendigits']['acc19']:.4f} uji {r['uji_digits']['acc19']:.4f})  "
            f"FAR@.9 uji {r['uji_neg']['far']:.4f} scrib {r['scribble']['far']:.4f} scr {r['scratch']['far']:.4f} "
            f"ovl {r['overlay']['far']:.4f} pair {r['pair']['far']:.4f}")
        if score > best:
            best = score
            torch.save({"arch": arch, "state": {k: v.cpu() for k, v in net.state_dict().items()}}, out / "best.pt")
    torch.save({"arch": arch, "state": {k: v.cpu() for k, v in net.state_dict().items()}}, out / "last.pt")
    (out / "history.json").write_text(json.dumps(hist, indent=1))
    return net


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--arch", default="c16-p-c32-p-c64-p-f128")
    ap.add_argument("--epochs", type=int, default=16)
    ap.add_argument("--steps", type=int, default=1200)
    ap.add_argument("--out", default="runs/base")
    ap.add_argument("--device", default="mps" if torch.backends.mps.is_available() else "cpu")
    ap.add_argument("--seed", type=int, default=0)
    ap.add_argument("--label-smoothing", type=float, default=0.0)
    ap.add_argument("--reject-boost", type=float, default=1.0,
                    help="multiply the sampling weight of every reject group")
    a = ap.parse_args()
    groups = {k: ((w * a.reject_boost if k not in ("mnist", "mnist_pen", "pen") else w), bm)
              for k, (w, bm) in GROUPS.items()}
    train(a.arch, a.epochs, a.out, torch.device(a.device), steps_per_epoch=a.steps, seed=a.seed,
          label_smoothing=a.label_smoothing, groups=groups)


if __name__ == "__main__":
    main()
