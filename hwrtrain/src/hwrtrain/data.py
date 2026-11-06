"""Dataset loaders. Raw downloads live in hwrtrain/data/<name>/ (gitignored).

Sources (see README for licenses):
  data/mnist/      MNIST idx.gz       (ossci-datasets S3 mirror of LeCun's files)
  data/emnist/     EMNIST gzip.zip    (NIST, biometrics.nist.gov/cs_links/EMNIST)
  data/pendigits/  pendigits-orig.{tra,tes}  UNIPEN strokes (UCI #81, via Wayback)
  data/uji2/       ujipenchars2.txt   (UCI #177, via Wayback)

Stroke samples are returned as lists of float32 (n,2) arrays in a y-DOWN frame
(same as the tablet screen).
"""

from __future__ import annotations

import gzip
import os
import re
from dataclasses import dataclass
from pathlib import Path

import numpy as np

ROOT = Path(os.environ.get("HWR_DATA", Path(__file__).resolve().parents[2] / "data"))
CACHE = ROOT / "cache"


def _idx(path: Path) -> np.ndarray:
    with gzip.open(path, "rb") as f:
        raw = f.read()
    ndim = raw[3]
    dims = [int.from_bytes(raw[4 + 4 * i: 8 + 4 * i], "big") for i in range(ndim)]
    return np.frombuffer(raw, np.uint8, offset=4 + 4 * ndim).reshape(dims)


def mnist(split: str):
    p = "train" if split == "train" else "t10k"
    x = _idx(ROOT / "mnist" / f"{p}-images-idx3-ubyte.gz")
    y = _idx(ROOT / "mnist" / f"{p}-labels-idx1-ubyte.gz").astype(np.int64)
    return x, y


def emnist(name: str, split: str):
    """EMNIST images are stored transposed relative to MNIST; fix on load.
    byclass labels: 0-9 digits, 10-35 'A'-'Z', 36-61 'a'-'z'."""
    d = ROOT / "emnist" / "gzip"
    x = _idx(d / f"emnist-{name}-{split}-images-idx3-ubyte.gz").transpose(0, 2, 1)
    y = _idx(d / f"emnist-{name}-{split}-labels-idx1-ubyte.gz").astype(np.int64)
    return np.ascontiguousarray(x), y


BYCLASS_CHARS = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"


@dataclass
class Ink:
    strokes: list          # list of float32 (n,2) arrays, y down
    label: str             # character
    writer: str
    source: str


def pendigits(split: str) -> list[Ink]:
    """UNIPEN-format pendigits-orig. Tablet y grows UPWARD -> flipped here.
    split: 'tra' (30 writers) or 'tes' (14 different writers)."""
    path = ROOT / "pendigits" / f"pendigits-orig.{split}"
    out: list[Ink] = []
    label = writer = None
    strokes: list = []
    cur: list | None = None

    def flush():
        if label is not None and strokes:
            out.append(Ink([s for s in strokes if len(s)], label, f"pd{split}{writer}", "pendigits"))

    for line in path.read_text().splitlines():
        if line.startswith(".SEGMENT"):
            flush()
            strokes = []
            label = re.search(r'"(.)"', line).group(1)
        elif line.startswith(".COMMENT"):
            writer = line.split()[2]
        elif line.startswith(".PEN_DOWN"):
            cur = []
        elif line.startswith(".PEN_UP"):
            if cur:
                a = np.array(cur, np.float32)
                a[:, 1] = -a[:, 1]
                strokes.append(a)
            cur = None
        elif cur is not None:
            parts = line.split()
            if len(parts) >= 2:
                cur.append((float(parts[0]), float(parts[1])))
    flush()
    return out


def uji2() -> list[Ink]:
    """UJIpenchars2: 60 writers x 97 chars x 2. Writer set encoded in the
    sample id: trn_* (40 writers) / tst_* (20 writers). Coordinates are
    already y-down screen coordinates."""
    path = ROOT / "uji2" / "ujipenchars2.txt"
    out: list[Ink] = []
    text = path.read_text(encoding="utf-8", errors="replace").replace("\r", "")
    lines = text.split("\n")
    i = 0
    while i < len(lines):
        ln = lines[i]
        if ln.startswith("WORD "):
            parts = ln.split()
            ch, sid = parts[1], parts[2]
            m = re.match(r"(trn|tst)_(\w+?)_W(\d+)-(\d+)", sid)
            writer = f"{m.group(1)}_{m.group(2)}_{m.group(3)}"
            n = int(lines[i + 1].split()[1])
            strokes = []
            for k in range(n):
                pl = lines[i + 2 + k]
                nums = pl.split("#", 1)[1].split()
                a = np.array(nums, np.float32).reshape(-1, 2)
                strokes.append(a)
            out.append(Ink(strokes, ch, writer, "uji2"))
            i += 2 + n
        else:
            i += 1
    return out
