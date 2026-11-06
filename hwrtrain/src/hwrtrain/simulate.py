"""Simulate the self-improvement loop with a held-out writer as "the player".

  python -I -m hwrtrain.simulate ../backend/hwr/model.bin [--writers 3]

For the pendigits-orig.tes writers (never seen in training) with the lowest
base accuracy: half of their 1-9 digits are written to a correction log in
the app's JSONL format, finetune.py adapts the model, and the other half is
scored before/after at the model's threshold.
"""

import argparse
import collections

import numpy as np
import torch

from . import data, raster
from .export import tabletize
from .finetune import decide, finetune
from .modelbin import read_model
from .samples import make_record, write_jsonl
from .train import predict


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("model")
    ap.add_argument("--writers", type=int, default=3)
    ap.add_argument("--steps", type=int, default=300)
    a = ap.parse_args()
    dev = torch.device("mps" if torch.backends.mps.is_available() else "cpu")
    mf = read_model(a.model)
    thr, net = mf.threshold, mf.net.to(dev)
    byw = collections.defaultdict(list)
    for s in data.pendigits("tes"):
        if s.label != "0":
            byw[s.writer].append(s)

    def score(n, samples):
        X = np.stack([raster.rasterize(s.strokes) for s in samples])
        d = decide(predict(n, np.clip(np.round(X * 255), 0, 255).astype(np.uint8), dev), thr)
        y = np.array([int(s.label) for s in samples])
        return float((d == y).mean()), float(((d > 0) & (d != y)).mean())

    ranked = sorted((score(net, v)[0], w) for w, v in byw.items())
    print("base accept-correct per test writer:", [(w, round(s, 3)) for s, w in ranked])
    rng = np.random.default_rng(0)
    for _, w in ranked[: a.writers]:
        ss = list(byw[w])
        rng.shuffle(ss)
        half = len(ss) // 2
        logged, held = ss[:half], ss[half:]
        path, out = f"/tmp/hwr_sim_{w}.jsonl", f"/tmp/hwr_sim_{w}.bin"
        write_jsonl(path, [make_record(tabletize(s.strokes, rng), int(s.label), ts=1000 * i) for i, s in enumerate(logged)])
        r = finetune([path], a.model, out, steps=a.steps, log=lambda m: None, force=True)
        b, bm = score(net, held)
        f, fm = score(read_model(out).net.to(dev), held)
        print(f"writer {w}: {len(logged)} logged, {len(held)} scored: accept-correct {b:.3f} -> {f:.3f}, "
              f"misread {bm:.3f} -> {fm:.3f}; writer-independent val FAR {r['before_base']['far']:.4f} -> "
              f"{r['after_base']['far']:.4f}")


if __name__ == "__main__":
    main()
