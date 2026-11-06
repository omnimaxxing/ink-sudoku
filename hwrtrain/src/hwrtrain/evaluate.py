"""Evaluate a model (checkpoint .pt or exported model.bin) and calibrate the
reject threshold.

  python -I -m hwrtrain.evaluate ../backend/hwr/model.bin
  python -I -m hwrtrain.evaluate runs/A/best.pt --calibrate

Accept rule (identical to Go Model.Classify): arg-max over all 11 outputs
must be a digit 1-9 with probability >= threshold.

Metrics
  acc19      forced choice among 1-9 (the classic accuracy number)
  accept     fraction of true digits accepted as the right digit at thr
  misread    fraction of true digits accepted as a WRONG digit at thr
  FAR        fraction of non-digits accepted as any digit at thr
"""

from __future__ import annotations

import argparse
import collections
import json

import numpy as np
import torch

from . import data
from .modelbin import REJECT, make_net, read_model
from .train import load_val, predict

NEG_VAL = ("uji_neg", "scribble", "scratch", "overlay", "pair")


def load_net(path):
    if str(path).endswith(".bin"):
        mf = read_model(path)
        return mf.net, mf.threshold
    ck = torch.load(path, map_location="cpu")
    net = make_net(ck["arch"])
    net.load_state_dict(ck["state"])
    return net.eval(), None


def decide(p, thr):
    best = p.argmax(1)
    ok = (best >= 1) & (best <= 9) & (p.max(1) >= thr)
    return np.where(ok, best, 0)


def load_test():
    xm, ym = data.mnist("test")
    t = np.load(data.CACHE / "test_pen.npz")
    em = np.load(data.CACHE / "emnist_letters.npz")
    sets = {"mnist_test": (xm, ym)}
    for k in ("pendigits", "uji_digits", "uji_neg", "uji_look"):
        sets[k] = (t[f"X_{k}"], t[f"y_{k}"].astype(np.int64))
    for k in ("scribble", "scratch", "overlay", "pair"):
        sets[k] = (t[f"X_{k}"], np.full(len(t[f"X_{k}"]), REJECT))
    sets["emnist_letters"] = (em["X_test"], np.full(len(em["X_test"]), REJECT))
    sets["emnist_look"] = (em["X_test_look"], np.full(len(em["X_test_look"]), REJECT))
    sk = np.load(data.CACHE / "skel.npz")
    sets["mnist_test_pen"] = (sk["mnist_test"], sk["mnist_test_y"].astype(np.int64))
    sets["emnist_pen"] = (sk["letters_test"], np.full(len(sk["letters_test"]), REJECT))
    sets["emnist_pen_look"] = (sk["look_test"], np.full(len(sk["look_test"]), REJECT))
    chars = {"uji_neg": t["chars_uji_neg"], "uji_look": t["chars_uji_look"],
             "emnist_letters": em["c_test"], "emnist_look": em["c_test_look"],
             "emnist_pen": sk["letters_test_c"], "emnist_pen_look": sk["look_test_c"]}
    return sets, chars


def calibrate(net, device, target_far=0.002, grid=None):
    """Pick the smallest threshold whose pooled pen-domain FAR (UJI letters/
    symbols + synthetic scribbles/scratch/overlay/pair) on the validation
    writers is <= target_far. (Scribble FAR is not required to hit 0: a
    few random scribbles genuinely look like digits.)"""
    val = load_val()
    P = {k: predict(net, v[0], device) for k, v in val.items()}
    grid = grid or [0.5, 0.7, 0.8, 0.9, 0.95, 0.97, 0.98, 0.99, 0.993, 0.995, 0.997, 0.998, 0.999]
    rows = []
    chosen = grid[-1]
    for thr in grid:
        far = {k: float((decide(P[k], thr) > 0).mean()) for k in NEG_VAL}
        n = sum(len(P[k]) for k in NEG_VAL)
        pooled = sum(far[k] * len(P[k]) for k in NEG_VAL) / n
        dig = []
        for k in ("pendigits", "uji_digits", "mnist_val"):
            y = val[k][1]
            m = (y >= 1) & (y <= 9)
            d = decide(P[k][m], thr)
            dig.append((float((d == y[m]).mean()), float(((d > 0) & (d != y[m])).mean())))
        rows.append(dict(thr=thr, pooled_far=pooled, **{f"far_{k}": v for k, v in far.items()},
                         pen_accept=dig[0][0], pen_misread=dig[0][1], uji_accept=dig[1][0],
                         mnist_accept=dig[2][0]))
    for r in rows:
        if r["pooled_far"] <= target_far:
            chosen = r["thr"]
            break
    return chosen, rows


def evaluate(net, thr, device, verbose=True):
    sets, chars = load_test()
    P = {k: predict(net, v[0], device) for k, v in sets.items()}
    out = {"threshold": thr}
    conf = collections.Counter()
    for k in ("mnist_test", "mnist_test_pen", "pendigits", "uji_digits"):
        y = sets[k][1]
        m = (y >= 1) & (y <= 9)
        p = P[k][m]
        forced = p[:, 1:10].argmax(1) + 1
        d = decide(p, thr)
        out[k] = dict(n=int(m.sum()), acc19=float((forced == y[m]).mean()),
                      accept=float((d == y[m]).mean()), misread=float(((d > 0) & (d != y[m])).mean()),
                      rejected=float((d == 0).mean()))
        if k in ("pendigits", "uji_digits"):
            for t, f in zip(y[m], forced):
                if t != f:
                    conf[(int(t), int(f))] += 1
        elif k == "mnist_test":
            out["mnist_confusions"] = collections.Counter(
                f"{t}->{f}" for t, f in zip(y[m], forced) if t != f).most_common(6)
    out["pen_confusions"] = [(f"{a}->{b}", n) for (a, b), n in conf.most_common(8)]
    for k in ("uji_neg", "uji_look", "scribble", "scratch", "overlay", "pair", "emnist_letters", "emnist_look",
              "emnist_pen", "emnist_pen_look"):
        d = decide(P[k], thr)
        out[k] = dict(n=len(d), far=float((d > 0).mean()))
        if k in chars:
            c = chars[k]
            per = {}
            for ch in sorted(set(c)):
                mm = c == ch
                per[ch] = (round(float((d[mm] > 0).mean()), 3), collections.Counter(d[mm][d[mm] > 0].tolist()).most_common(1))
            worst = sorted(per.items(), key=lambda kv: -kv[1][0])[:8]
            out[k]["worst"] = [(ch, f, (top[0][0] if top else None)) for ch, (f, top) in worst if f > 0]
    if verbose:
        print(json.dumps(out, indent=1, default=str))
    return out


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("model")
    ap.add_argument("--thr", type=float, default=None)
    ap.add_argument("--calibrate", action="store_true")
    ap.add_argument("--device", default="mps" if torch.backends.mps.is_available() else "cpu")
    a = ap.parse_args()
    dev = torch.device(a.device)
    net, thr = load_net(a.model)
    net = net.to(dev)
    if a.calibrate or (thr is None and a.thr is None):
        thr, rows = calibrate(net, dev)
        for r in rows:
            print({k: round(v, 4) for k, v in r.items()})
        print("calibrated threshold", thr)
    if a.thr is not None:
        thr = a.thr
    evaluate(net, thr, dev)


if __name__ == "__main__":
    main()
