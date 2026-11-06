"""Pre-render every training/eval image into data/cache/*.npz.

  python -I -m hwrtrain.build

Splits are writer-disjoint:
  train  MNIST train; pendigits-orig.tra writers 1-25; UJI2 trn writers
         except every 5th; UJI2 letters/symbols of those writers; EMNIST
         byclass *train* letters; synthetic negatives (seed 1).
  val    pendigits writers 26-30; UJI2 trn every-5th writers (8 writers);
         their letters/symbols; synthetic negatives (seed 2). Used to pick
         the reject threshold.
  test   MNIST test; pendigits-orig.tes (14 writers); UJI2 tst writers (20);
         EMNIST byclass *test* letters; synthetic negatives (seed 3).

Class ids: 0-9 digits, 10 reject. Letters that legitimately resemble a
digit (LOOKALIKE) are never used as reject training data; they are only
evaluated, and reported separately.
"""

from __future__ import annotations

import multiprocessing as mp
import sys

import numpy as np
from scipy import ndimage
from skimage.morphology import skeletonize
from skimage.transform import resize

from . import data, raster, synth

REJECT = 10
# Chars that can legitimately look like 1-9 (l/1, S/5, Z/2, g/9, b/6, B/8, ...)
LOOKALIKE = set("IlSsZzGgqbB") | set("'\",()!¡$|")
PD_VAL_WRITERS = {str(w) for w in range(26, 31)}


def uji_val_writers(samples):
    trn = sorted({s.writer for s in samples if s.writer.startswith("trn")})
    return set(trn[::5])


def _render_job(args):
    strokes, n_aug, seed, label = args
    rng = np.random.default_rng(seed)
    out = []
    for k in range(n_aug):
        if k == 0 and n_aug > 1:
            st, r = strokes, raster.RADIUS
        else:
            st, r = synth.augment(strokes, rng), synth.random_radius(rng)
        out.append(raster.rasterize(st, r))
    return np.stack(out), np.full(len(out), label, np.int16)


def _render_clean(args):
    strokes, label = args
    return raster.rasterize(strokes)[None], np.array([label], np.int16)


_POOL: list = []
KIND_ID = dict(scribble=1, scratch=2, overlay=3, pair=4)


def _set_pool(p):
    global _POOL
    _POOL = p


def _neg_job(args):
    kind, seed = args
    pool = _POOL
    rng = np.random.default_rng(seed)
    if kind == "scribble":
        st = synth.scribble(rng)
    elif kind == "scratch":
        st = synth.scratch(rng)
    elif kind == "overlay":
        base = pool[rng.integers(len(pool))]
        st = synth.overlay(base, synth.scratch(rng), rng)
    else:  # pair
        a = pool[rng.integers(len(pool))]
        b = pool[rng.integers(len(pool))]
        st = synth.pair(a, b, rng)
    if rng.random() < 0.5:
        st = synth.augment(st, rng, 0.5)
    return raster.rasterize(st, synth.random_radius(rng))


def label_of(ch: str) -> int:
    if ch.isdigit():
        return int(ch)
    return REJECT


def to_u8(x):
    return np.clip(np.round(np.asarray(x) * 255), 0, 255).astype(np.uint8)


def normalize_bitmap(img):
    """Re-normalise a scanned glyph MNIST-style: crop ink, fit 20x20
    preserving aspect, centre of mass at raster.COM. Used for EMNIST, whose
    own normalisation (24px box, blur) differs from MNIST's."""
    img = img.astype(np.float32) / 255.0
    ys, xs = np.where(img > 0.1)
    if len(ys) == 0:
        return np.zeros((28, 28), np.float32)
    crop = img[ys.min(): ys.max() + 1, xs.min(): xs.max() + 1]
    h, w = crop.shape
    s = 20.0 / max(h, w)
    nh, nw = max(1, round(h * s)), max(1, round(w * s))
    crop = resize(crop, (nh, nw), anti_aliasing=True, preserve_range=True).astype(np.float32)
    canvas = np.zeros((28, 28), np.float32)
    tot = crop.sum()
    cy = (crop.sum(1) * (np.arange(nh) + 0.5)).sum() / tot
    cx = (crop.sum(0) * (np.arange(nw) + 0.5)).sum() / tot
    oy, ox = raster.COM_Y - cy, raster.COM_X - cx
    iy, ix = int(np.floor(oy)), int(np.floor(ox))
    y0, x0 = max(0, iy), max(0, ix)
    y1, x1 = min(28, iy + nh), min(28, ix + nw)
    canvas[y0:y1, x0:x1] = crop[y0 - iy: y1 - iy, x0 - ix: x1 - ix]
    canvas = ndimage.shift(canvas, (oy - iy, ox - ix), order=1, mode="constant")
    return np.clip(canvas, 0, 1)


def skeleton_strokes(img, up=4):
    """Turn a scanned glyph into pen-like strokes: upsample, binarise,
    skeletonise, and connect 8-neighbour skeleton pixels with 2-point
    segments. Rasterising these gives MNIST/EMNIST shapes with exactly the
    on-tablet stroke profile."""
    big = ndimage.zoom(img.astype(np.float32) / 255.0, up, order=1)
    sk = skeletonize(big > 0.45)
    ys, xs = np.nonzero(sk)
    if len(ys) == 0:
        return []
    idx = -np.ones(sk.shape, int)
    idx[ys, xs] = np.arange(len(ys))
    segs = []
    for dy, dx in ((0, 1), (1, 0), (1, 1), (1, -1)):
        y2, x2 = ys + dy, xs + dx
        ok = (y2 < sk.shape[0]) & (x2 >= 0) & (x2 < sk.shape[1])
        ok[ok] &= sk[y2[ok], x2[ok]]
        for a, b in zip(np.nonzero(ok)[0], idx[y2[ok], x2[ok]]):
            segs.append(np.array([[xs[a], ys[a]], [xs[b], ys[b]]], np.float32))
    if not segs:
        segs = [np.array([[x, y]], np.float32) for x, y in zip(xs, ys)]
    return segs


def _skel_job(args):
    img, seed, aug = args
    rng = np.random.default_rng(seed)
    st = skeleton_strokes(img)
    if not st:
        return np.zeros((28, 28), np.float32)
    if aug:
        st = synth.augment(st, rng, 0.8, jitter=0, trim=False)
        return raster.rasterize(st, synth.random_radius(rng))
    return raster.rasterize(st)


def skeletonized(imgs, seed, aug):
    jobs = [(im, seed * 1_000_003 + i, aug) for i, im in enumerate(imgs)]
    with mp.Pool() as pool:
        return to_u8(np.stack(pool.map(_skel_job, jobs, chunksize=128)))


def build_skel():
    """data/cache/skel.npz: MNIST + EMNIST letters re-drawn as pen strokes."""
    xm, ym = data.mnist("train")
    xt, yt = data.mnist("test")
    rep = 2
    mn = np.concatenate([skeletonized(xm[:55000], 21 + r, True) for r in range(rep)])
    mny = np.tile(ym[:55000], rep)
    mnt = skeletonized(xt, 22, False)
    keep = set(data.BYCLASS_CHARS[10:]) - LOOKALIKE
    look = set(data.BYCLASS_CHARS[10:]) & LOOKALIKE

    def letters(split, chars, limit, seed, aug):
        x, y = data.emnist("byclass", split)
        c = np.array(list(data.BYCLASS_CHARS))[y]
        idx = np.where(np.isin(c, list(chars)))[0]
        idx = np.random.default_rng(seed).choice(idx, min(limit, len(idx)), replace=False)
        return skeletonized(x[idx], seed, aug), c[idx]

    el, cl = letters("train", keep, 90000, 31, True)
    et, ct = letters("test", keep, 30000, 32, False)
    etl, ctl = letters("test", look, 10000, 33, False)
    np.savez_compressed(data.CACHE / "skel.npz", mnist_train=mn, mnist_train_y=mny, mnist_test=mnt, mnist_test_y=yt,
                        letters_train=el, letters_train_c=cl, letters_test=et, letters_test_c=ct,
                        look_test=etl, look_test_c=ctl)
    print("skel", mn.shape, mnt.shape, el.shape, et.shape, etl.shape)


def _norm_job(img):
    # EMNIST is Gaussian-blurred; stretch contrast toward MNIST's crisper
    # edge profile so "blurry" is not a cue for the reject class.
    return np.clip((normalize_bitmap(img) - 0.15) / 0.7, 0, 1)


def emnist_letters(split, keep, limit, seed):
    x, y = data.emnist("byclass", split)
    chars = np.array(list(data.BYCLASS_CHARS))[y]
    m = np.array([c in keep for c in chars])
    idx = np.where(m)[0]
    rng = np.random.default_rng(seed)
    if limit and len(idx) > limit:
        idx = rng.choice(idx, limit, replace=False)
    with mp.Pool() as pool:
        imgs = pool.map(_norm_job, list(x[idx]), chunksize=256)
    return to_u8(np.stack(imgs)), chars[idx]


def render_set(pool, items, n_aug, seed):
    jobs = [(s.strokes, n_aug, seed * 1_000_003 + i, label_of(s.label)) for i, s in enumerate(items)]
    res = pool.map(_render_job if n_aug > 1 else _render_clean,
                   jobs if n_aug > 1 else [(j[0], j[3]) for j in jobs], chunksize=32)
    X = np.concatenate([r[0] for r in res]) if res else np.zeros((0, 28, 28))
    y = np.concatenate([r[1] for r in res]) if res else np.zeros(0, np.int16)
    return to_u8(X), y


def negatives(counts, seed, digit_pool):
    out = {}
    with mp.Pool(initializer=_set_pool, initargs=(digit_pool,)) as pool:
        for kind, n in counts.items():
            jobs = [(kind, (seed * 1_000_003 + KIND_ID[kind]) * 1_000_003 + i) for i in range(n)]
            out[kind] = to_u8(np.stack(pool.map(_neg_job, jobs, chunksize=64)))
    return out


def main():
    data.CACHE.mkdir(parents=True, exist_ok=True)
    pd_tra, pd_tes = data.pendigits("tra"), data.pendigits("tes")
    uji = data.uji2()
    uval = uji_val_writers(uji)

    def uji_split(s):
        if s.writer.startswith("tst"):
            return "test"
        return "val" if s.writer in uval else "train"

    pd_train = [s for s in pd_tra if s.writer[5:] not in PD_VAL_WRITERS]
    pd_val = [s for s in pd_tra if s.writer[5:] in PD_VAL_WRITERS]
    u = {k: [s for s in uji if uji_split(s) == k] for k in ("train", "val", "test")}
    udig = {k: [s for s in v if s.label.isdigit()] for k, v in u.items()}
    uneg = {k: [s for s in v if not s.label.isdigit() and s.label not in LOOKALIKE] for k, v in u.items()}
    ulook = {k: [s for s in v if not s.label.isdigit() and s.label in LOOKALIKE] for k, v in u.items()}
    print("pendigits train/val/test", len(pd_train), len(pd_val), len(pd_tes))
    print("uji digits", {k: len(v) for k, v in udig.items()}, "neg", {k: len(v) for k, v in uneg.items()},
          "lookalike", {k: len(v) for k, v in ulook.items()})
    sys.stdout.flush()

    with mp.Pool() as pool:
        # ---- train
        Xd, yd = render_set(pool, pd_train + udig["train"], 24, 11)
        Xu, yu = render_set(pool, uneg["train"], 8, 12)
        digit_pool = [s.strokes for s in pd_train + udig["train"]]
        neg = negatives(dict(scribble=40000, scratch=12000, overlay=10000, pair=14000), 1, digit_pool)
        np.savez_compressed(data.CACHE / "train_pen.npz", X=Xd, y=yd, Xneg_uji=Xu, **{f"Xneg_{k}": v for k, v in neg.items()})
        print("train pen digits", Xd.shape, "uji neg", Xu.shape, {k: v.shape for k, v in neg.items()})
        sys.stdout.flush()

        # ---- val / test (clean, unaugmented renders)
        for split, pdset, seed in (("val", pd_val, 2), ("test", pd_tes, 3)):
            sets = {
                "pendigits": render_set(pool, pdset, 1, 0),
                "uji_digits": render_set(pool, udig[split], 1, 0),
                "uji_neg": render_set(pool, uneg[split], 1, 0),
                "uji_look": render_set(pool, ulook[split], 1, 0),
            }
            dp = [s.strokes for s in pdset + udig[split]]
            neg = negatives(dict(scribble=4000, scratch=2000, overlay=2000, pair=2000), seed, dp)
            arrs = {}
            for k, (X, y) in sets.items():
                arrs[f"X_{k}"], arrs[f"y_{k}"] = X, y
            for k, v in neg.items():
                arrs[f"X_{k}"] = v
            arrs["chars_uji_neg"] = np.array([s.label for s in uneg[split]])
            arrs["chars_uji_look"] = np.array([s.label for s in ulook[split]])
            np.savez_compressed(data.CACHE / f"{split}_pen.npz", **arrs)
            print(split, {k: v.shape for k, v in arrs.items()})
            sys.stdout.flush()

    keep = set(data.BYCLASS_CHARS[10:]) - LOOKALIKE
    look = set(data.BYCLASS_CHARS[10:]) & LOOKALIKE
    Xl, cl = emnist_letters("train", keep, 90000, 5)
    Xt, ct = emnist_letters("test", keep, 30000, 6)
    Xtl, ctl = emnist_letters("test", look, 10000, 7)
    np.savez_compressed(data.CACHE / "emnist_letters.npz", X_train=Xl, c_train=cl, X_test=Xt, c_test=ct,
                        X_test_look=Xtl, c_test_look=ctl)
    print("emnist letters", Xl.shape, Xt.shape, Xtl.shape)


if __name__ == "__main__":
    if sys.argv[1:] == ["skel"]:
        build_skel()
    else:
        main()
        build_skel()
