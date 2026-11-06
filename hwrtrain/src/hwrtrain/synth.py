"""Stroke-space augmentation and synthetic negatives.

Everything here produces *strokes* (lists of (n,2) float32 arrays, y down)
that are then rasterised by raster.rasterize, so they share the exact
inference-time image pipeline.
"""

from __future__ import annotations

import numpy as np

TAU = 2 * np.pi


def _norm(strokes):
    pts = np.concatenate(strokes)
    lo, hi = pts.min(0), pts.max(0)
    c = (lo + hi) / 2
    size = max(float((hi - lo).max()), 1e-6)
    return [(s - c) / size for s in strokes]


def densify(strokes, step=0.015):
    """Insert points so consecutive points are <= step apart (unit-size
    glyph). Warps then bend long straight segments smoothly."""
    out = []
    for s in strokes:
        if len(s) < 2:
            out.append(s)
            continue
        pieces = [s[:1]]
        for a, b in zip(s[:-1], s[1:]):
            n = int(np.ceil(np.linalg.norm(b - a) / step))
            if n > 1:
                t = np.linspace(0, 1, n + 1, dtype=np.float32)[1:, None]
                pieces.append(a + (b - a) * t)
            else:
                pieces.append(b[None])
        out.append(np.concatenate(pieces).astype(np.float32))
    return out


def _elastic(p, rng, amp):
    """Smooth random displacement: sum of a few random low-frequency waves."""
    d = np.zeros_like(p)
    for _ in range(3):
        f = rng.normal(0, 2.2, size=2)
        ph = rng.uniform(0, TAU)
        dirv = rng.normal(0, 1, size=2)
        dirv /= np.linalg.norm(dirv) + 1e-9
        d += np.sin(p @ f * TAU / 2 + ph)[:, None] * dirv[None] * amp * rng.uniform(0.3, 1)
    return p + d


def augment(strokes, rng, strength=1.0, jitter=0.003, trim=True):
    """Pen-ink augmentation: slant, rotation, aspect, elastic warp, jitter,
    occasional dropped/trimmed stroke ends. Output in unit-ish coords.
    The warp is a function of position only, so skeleton segments that
    share endpoints stay connected when jitter=0."""
    s = densify(_norm([np.asarray(x, np.float32)[:, :2] for x in strokes]))
    rot = np.clip(rng.normal(0, 0.10), -0.26, 0.26) * strength
    shear = rng.uniform(-0.35, 0.35) * strength
    sx = np.exp(rng.normal(0, 0.12) * strength)
    sy = np.exp(rng.normal(0, 0.06) * strength)
    c, sn = np.cos(rot), np.sin(rot)
    A = np.array([[c, -sn], [sn, c]], np.float32) @ np.array([[1, shear], [0, 1]], np.float32) @ np.diag([sx, sy]).astype(np.float32)
    amp = 0.035 * strength
    out = []
    for st in s:
        q = st @ A.T
        q = _elastic(q, rng, amp)
        if jitter:
            q = q + rng.normal(0, jitter, q.shape)
        # trim/extend ends slightly
        if trim and len(q) > 8 and rng.random() < 0.3 * strength:
            k = rng.integers(0, max(1, len(q) // 12))
            q = q[k:] if rng.random() < 0.5 else q[: len(q) - k]
        out.append(q.astype(np.float32))
    return out


def random_radius(rng):
    """Stroke half-width in output px, spanning MNIST's width spread."""
    return float(np.clip(rng.normal(1.15, 0.22), 0.75, 1.8))


# ---------------------------------------------------------------- negatives

def _catmull(ctrl, n=80):
    p = np.concatenate([ctrl[:1], ctrl, ctrl[-1:]])
    out = []
    for i in range(1, len(p) - 2):
        p0, p1, p2, p3 = p[i - 1], p[i], p[i + 1], p[i + 2]
        t = np.linspace(0, 1, max(4, n // (len(p) - 3)), endpoint=False)[:, None]
        out.append(0.5 * ((2 * p1) + (-p0 + p2) * t + (2 * p0 - 5 * p1 + 4 * p2 - p3) * t ** 2
                          + (-p0 + 3 * p1 - 3 * p2 + p3) * t ** 3))
    out.append(p[-2][None])
    return np.concatenate(out).astype(np.float32)


def scribble(rng):
    """Random non-digit ink: wandering curves, loops, hashes, spirals."""
    kind = rng.integers(0, 6)
    if kind == 0:   # long wandering spline through many points
        k = rng.integers(7, 13)
        return [_catmull(rng.uniform(-1, 1, (k, 2)).astype(np.float32), 160)]
    if kind == 1:   # random walk with momentum
        n = rng.integers(120, 260)
        ang = np.cumsum(rng.normal(0, 0.35, n)) + rng.uniform(0, TAU)
        step = rng.uniform(0.02, 0.05)
        p = np.cumsum(np.stack([np.cos(ang), np.sin(ang)], 1) * step, 0)
        return [p.astype(np.float32)]
    if kind == 2:   # several short random strokes (hash, crosses, ticks)
        out = []
        for _ in range(rng.integers(3, 6)):
            a = rng.uniform(-1, 1, 2)
            b = a + rng.normal(0, 0.7, 2)
            out.append(np.linspace(a, b, 12).astype(np.float32))
        return out
    if kind == 3:   # spiral / multi-turn loop
        turns = rng.uniform(2, 5)
        t = np.linspace(0, turns * TAU, 220)
        r = np.linspace(rng.uniform(0.05, 0.3), 1, len(t)) * (1 + 0.15 * rng.normal(0, 1, len(t)).cumsum() / 30)
        p = np.stack([r * np.cos(t) * rng.uniform(0.6, 1.4), r * np.sin(t)], 1)
        return [p.astype(np.float32)]
    if kind == 4:   # cursive-like loops ("eeee") drifting sideways
        n = rng.integers(3, 7)
        t = np.linspace(0, n * TAU, 60 * n)
        rx, ry = rng.uniform(0.3, 0.6), rng.uniform(0.5, 1)
        p = np.stack([t / (n * TAU) * 2 + rx * np.cos(t), ry * np.sin(t)], 1)
        return [p.astype(np.float32)]
    # several overlapping closed blobs / curves in different places
    out = []
    for _ in range(rng.integers(2, 4)):
        k = rng.integers(4, 7)
        c = rng.uniform(-0.6, 0.6, 2)
        out.append(_catmull((c + rng.normal(0, 0.45, (k, 2))).astype(np.float32), 60))
    return out


def scratch(rng):
    """Zig-zag scratch-out: back-and-forth strokes, optionally over a box."""
    n = rng.integers(4, 13)                      # number of legs
    amp = 1.0
    drift = rng.uniform(0.6, 1.6) / n            # advance per leg
    pts = []
    for i in range(n + 1):
        x = (amp if i % 2 else -amp) + rng.normal(0, 0.08)
        y = i * drift + rng.normal(0, drift * 0.15)
        pts.append((x, y))
    p = np.array(pts, np.float32)
    if rng.random() < 0.5:   # round the corners a little
        p = _catmull(p, 30 * n)
    rot = rng.uniform(0, TAU)
    c, s = np.cos(rot), np.sin(rot)
    p = p @ np.array([[c, -s], [s, c]], np.float32).T
    return [p]


def overlay(base, extra, rng):
    """Combine two stroke sets, extra scaled to cover base."""
    b = _norm([np.asarray(x, np.float32)[:, :2] for x in base])
    e = _norm(extra)
    k = rng.uniform(0.7, 1.2)
    off = rng.normal(0, 0.1, 2).astype(np.float32)
    return b + [x * k + off for x in e]


def pair(a, b, rng):
    """Two glyphs side by side (a mis-grouped pair of pencil marks)."""
    a = _norm([np.asarray(x, np.float32)[:, :2] for x in a])
    b = _norm([np.asarray(x, np.float32)[:, :2] for x in b])
    gap = rng.uniform(0.15, 0.6)
    dy = rng.normal(0, 0.15)
    if rng.random() < 0.75:
        return a + [x + np.array([1 + gap, dy], np.float32) for x in b]
    return a + [x + np.array([dy, 1 + gap], np.float32) for x in b]
