"""Reference rasterizer. MUST stay bit-for-bit equivalent (up to float rounding)
to backend/hwr/raster.go -- every stroke-sourced training image goes through
this function and every on-tablet inference goes through the Go one.

Algorithm (MNIST/EMNIST-style normalisation, but from pen strokes):

1. Bounding box of all stroke points (centre-line, screen pixels).
2. Uniform scale s = (BOX - 2*(R+SOFT)) / max(w, h): the *inked* glyph
   (centre-line bbox grown by the full stroke footprint R+SOFT) fits a
   BOX x BOX (20x20) square with aspect preserved. R is in output pixels,
   so stroke width scales with the glyph, not with the physical pen width.
3. Each segment is drawn as a capsule with an anti-aliased edge:
       coverage(d) = clamp((R + SOFT - d) / (2*SOFT), 0, 1)
   where d is the distance from the pixel centre to the segment. Overlapping
   segments combine with max (ink is a union, never "darker" where strokes
   cross). Pen pressure is ignored, MNIST is binary ink.
4. Render once with the bbox centred, measure the intensity centre of mass,
   and re-render with the sub-pixel translation that puts the centre of mass
   at (COM_X, COM_Y) (continuous coords, pixel (r, c) centre = (c+.5, r+.5)).

Constants below are mirrored in raster.go (RasterRadius etc).
"""

from __future__ import annotations

import numpy as np

SIZE = 28
BOX = 20.0
RADIUS = 1.1   # default stroke half-width, output px (matches MNIST median width)
SOFT = 0.5     # half-width of the anti-aliased edge ramp, output px
COM_X = 14.5   # MNIST mean centre of mass (continuous coords = pixel index 14)
COM_Y = 14.5

_PC = (np.arange(SIZE, dtype=np.float32) + np.float32(0.5))
_PX = np.tile(_PC, SIZE)            # pixel-centre x, row-major
_PY = np.repeat(_PC, SIZE)          # pixel-centre y


def _segments(strokes):
    """Flatten strokes into (N,4) float32 segments x0,y0,x1,y1. A single-point
    stroke becomes a zero-length segment (a dot)."""
    segs = []
    for s in strokes:
        a = np.asarray(s, dtype=np.float32)
        if a.ndim != 2 or len(a) == 0:
            continue
        a = a[:, :2]
        if len(a) == 1:
            segs.append(np.concatenate([a, a], axis=1))
        else:
            segs.append(np.concatenate([a[:-1], a[1:]], axis=1))
    if not segs:
        return np.zeros((0, 4), np.float32)
    return np.concatenate(segs, axis=0)


def _render(seg, radius, soft):
    """seg: (N,4) float32 in output pixel coords. Returns (784,) float32."""
    out = np.zeros(SIZE * SIZE, np.float32)
    if len(seg) == 0:
        return out
    r = np.float32(radius)
    sf = np.float32(soft)
    # process in chunks to bound memory for very long inputs
    for i in range(0, len(seg), 256):
        s = seg[i:i + 256]
        x0, y0, x1, y1 = (s[:, k:k + 1] for k in range(4))
        dx = x1 - x0
        dy = y1 - y0
        ll = dx * dx + dy * dy
        px = _PX[None, :] - x0
        py = _PY[None, :] - y0
        with np.errstate(divide="ignore", invalid="ignore"):
            t = np.where(ll > 0, (px * dx + py * dy) / np.where(ll > 0, ll, 1), 0)
        t = np.clip(t, 0, 1).astype(np.float32)
        ex = px - t * dx
        ey = py - t * dy
        d = np.sqrt(ex * ex + ey * ey)
        c = np.clip((r + sf - d) / (2 * sf), 0, 1)
        np.maximum(out, c.max(axis=0), out=out)
    return out


def rasterize(strokes, radius: float = RADIUS, soft: float = SOFT) -> np.ndarray:
    """strokes: list of (n,>=2) arrays of x,y (screen px, y down).
    Returns float32 (28,28) in [0,1]."""
    seg = _segments(strokes)
    if len(seg) == 0:
        return np.zeros((SIZE, SIZE), np.float32)
    xs = np.concatenate([seg[:, 0], seg[:, 2]])
    ys = np.concatenate([seg[:, 1], seg[:, 3]])
    minx, maxx = np.float32(xs.min()), np.float32(xs.max())
    miny, maxy = np.float32(ys.min()), np.float32(ys.max())
    w = maxx - minx
    h = maxy - miny
    ext = max(w, h)
    fit = np.float32(BOX - 2 * (radius + soft))
    scale = np.float32(fit / ext) if ext > 1e-6 else np.float32(0)
    half = np.float32(SIZE / 2)
    ox = half - w * scale / 2
    oy = half - h * scale / 2
    m = seg.copy()
    m[:, [0, 2]] = (seg[:, [0, 2]] - minx) * scale + ox
    m[:, [1, 3]] = (seg[:, [1, 3]] - miny) * scale + oy
    img = _render(m, radius, soft)
    tot = img.sum(dtype=np.float32)
    if tot > 0:
        cx = (img * _PX).sum(dtype=np.float32) / tot
        cy = (img * _PY).sum(dtype=np.float32) / tot
        m[:, [0, 2]] += np.float32(COM_X) - cx
        m[:, [1, 3]] += np.float32(COM_Y) - cy
        img = _render(m, radius, soft)
    return img.reshape(SIZE, SIZE)


def center_of_mass(img: np.ndarray):
    img = np.asarray(img, np.float64).reshape(SIZE, SIZE)
    tot = img.sum()
    if tot <= 0:
        return (np.nan, np.nan)
    pc = np.arange(SIZE) + 0.5
    return (float((img.sum(0) * pc).sum() / tot), float((img.sum(1) * pc).sum() / tot))
