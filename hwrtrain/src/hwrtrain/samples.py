"""Reader/writer for the correction log (JSONL). Format documented in
backend/hwr/sample.go (type Sample); one JSON object per line:

  {"v":1,"ts":..,"model":"9f3a12bc","source":"correction","kind":"candidate",
   "label":7,"pred":1,"conf":0.93,"top3":[[1,0.93],[7,0.05],[4,0.01]],
   "cell":[x0,y0,x1,y1],"strokes":[[[x,y,p,t],...],...]}

label is the TRUE value: 1..9, or 0 = "not a digit" (trained as reject).
"""

from __future__ import annotations

import json
from dataclasses import dataclass

import numpy as np


@dataclass
class Correction:
    strokes: list   # list of (n,2) float32 screen px
    label: int      # 1..9 or 0 (= not a digit)
    pred: int
    source: str
    ts: int
    raw: dict


def read_jsonl(path) -> list[Correction]:
    out = []
    with open(path) as f:
        for n, line in enumerate(f, 1):
            line = line.strip()
            if not line:
                continue
            d = json.loads(line)
            if d.get("v") != 1:
                raise ValueError(f"{path}:{n}: unsupported version {d.get('v')}")
            st = [np.asarray(s, np.float32)[:, :2] for s in d["strokes"] if len(s)]
            if not st:
                continue
            lab = int(d["label"])
            if not 0 <= lab <= 9:
                raise ValueError(f"{path}:{n}: label {lab} out of range")
            out.append(Correction(st, lab, int(d.get("pred", 0)), d.get("source", ""), int(d.get("ts", 0)), d))
    return out


def write_jsonl(path, records):
    """records: iterables of dicts already in the Sample shape."""
    with open(path, "w") as f:
        for r in records:
            f.write(json.dumps(r, separators=(",", ":")) + "\n")


def make_record(strokes_xy, label, pred=0, conf=0.0, source="correction", ts=0, cell=(0, 0, 0, 0), kind="unknown"):
    """Build a Sample-shaped dict from (n,2) strokes (pressure/time filled)."""
    st = []
    t = ts
    for s in strokes_xy:
        st.append([[float(x), float(y), 0.5, float(t + 4 * i)] for i, (x, y) in enumerate(np.asarray(s)[:, :2])])
        t += 4 * len(s) + 250
    return {"v": 1, "ts": ts, "model": "", "source": source, "kind": kind, "label": int(label),
            "pred": int(pred), "conf": float(conf), "top3": [], "cell": list(cell), "strokes": st}
