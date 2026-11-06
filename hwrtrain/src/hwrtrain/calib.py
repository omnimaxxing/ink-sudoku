"""Export pen strokes for calibrating Analyze's geometric thresholds.

  python -I -m hwrtrain.calib        -> data/cache/calib_strokes.jsonl

Each line: {"src": ..., "label": char, "strokes": [[[x,y],...],...]} for all
pendigits + UJI2 samples and synthetic zig-zag scratch-outs (label "#").
The Go side reads it in TestScratchCalibration (HWR_CALIB=<path>).

Also writes e2e_strokes.jsonl (held-out writers, for TestEndToEnd) and
personal_strokes.jsonl (for TestPersonalSimulation, HWR_PERSONAL=<path>):
every sample of the writers never trained on, tagged with "split": "dev"
(pendigits val writers 26-30, UJI2 val writers -- tune Personal here) or
"test" (pendigits-orig.tes, UJI2 tst -- report here).
"""

import json

import numpy as np

from . import data, synth
from .build import PD_VAL_WRITERS, uji_val_writers


def main():
    out = data.CACHE / "calib_strokes.jsonl"
    rng = np.random.default_rng(42)
    with open(out, "w") as f:
        for s in data.pendigits("tra") + data.pendigits("tes") + data.uji2():
            f.write(json.dumps({"src": s.source, "label": s.label,
                                "strokes": [np.round(x, 2).tolist() for x in s.strokes]}) + "\n")
        for _ in range(3000):
            st = [x * 60 for x in synth.scratch(rng)]
            f.write(json.dumps({"src": "synth", "label": "#", "strokes": [np.round(x, 2).tolist() for x in st]}) + "\n")
    print(out)
    # held-out writers only, for the end-to-end Analyze test (HWR_E2E)
    e2e = data.CACHE / "e2e_strokes.jsonl"
    with open(e2e, "w") as f:
        for s in data.pendigits("tes") + [u for u in data.uji2() if u.writer.startswith("tst")]:
            f.write(json.dumps({"src": s.source, "writer": s.writer, "label": s.label,
                                "strokes": [np.round(x, 2).tolist() for x in s.strokes]}) + "\n")
    print(e2e)
    uji = data.uji2()
    uval = uji_val_writers(uji)
    dev = [s for s in data.pendigits("tra") if s.writer[5:] in PD_VAL_WRITERS] + [s for s in uji if s.writer in uval]
    test = data.pendigits("tes") + [s for s in uji if s.writer.startswith("tst")]
    pers = data.CACHE / "personal_strokes.jsonl"
    with open(pers, "w") as f:
        for split, ss in (("dev", dev), ("test", test)):
            for s in ss:
                f.write(json.dumps({"split": split, "src": s.source, "writer": s.writer, "label": s.label,
                                    "strokes": [np.round(x, 2).tolist() for x in s.strokes]}) + "\n")
    print(pers)


if __name__ == "__main__":
    main()
