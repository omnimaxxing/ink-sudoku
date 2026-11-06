// Canned Ink Sudoku backend for the Mac stub: answers the frontend's messages with
// PROTOCOL.md-shaped replies, using the puzzle and entries from PROTOCOL.md's example.
// The player's handwriting is synthesized from a tiny single-stroke digit font.
// Flags (after `--` on the qml6 command line):
//   --hint       also send a hint after `state`
//   --daily      the restored game is today's daily puzzle
//   --solved     the restored game is solved, and `solved` follows `state`
//   --first-run  personalSamples is 0 (the frontend nudges toward calibration)
// Calibration: calibrationStart/Ink/Done answer like the backend (1–9, then two fixed
// shuffles; `calibration` after every `calibrationRead`); every fifth sample is misread. A setCell that completes the grid sends `solved`.
.pragma library

const GRID = 1180, S = GRID / 9;
const GIVENS = "37.94.5.....2..7.....3...98.54.....7.........8.....93.24...9.....6..2.....8.71.25";
const ENTRIES = "..1..8.62589.16.43462.571..6............2...6.27.................................";
// One extra answer that repeats the 6 in column 9, so conflicts show.
const EXTRA = { 53: 6 };
const PENCIL = { 30: [1, 8], 33: [2, 8], 34: [1, 8], 36: [1, 9], 50: [4, 5] };
// Typed through the setCell / toggleCandidate fallback, so no ink: shows the typeset fallback.
const TYPED_ENTRY = 8;
const TYPED_CANDIDATES = { 63: [1, 3, 7] };
const HINT = {
    technique: "Hidden single", houses: [22], cells: [39], digits: [7],
    placement: { cell: 39, digit: 7 }, title: "Only one place for the 7",
    text: "In box 5, the 7 can only go in row 5, column 4. Every other spot is ruled out."
};
const STATS = {
    easy: { played: 3, solved: 2, bestMs: 312000, streak: 2 },
    medium: { played: 5, solved: 5, bestMs: 545000, streak: 5 },
    hard: { played: 4, solved: 2, bestMs: 860000, streak: 2 },
    expert: { played: 1, solved: 0, bestMs: 0, streak: 0 },
    daily: { streak: 4, last: "" }
};
// Daily level by weekday, Sunday first (PROTOCOL v2).
const DAILY_LEVELS = ["expert", "easy", "easy", "medium", "medium", "hard", "hard"];
// Calibration order (backend): 1–9, then two fixed shuffles.
const CALIB_ORDER = [1, 2, 3, 4, 5, 6, 7, 8, 9, 3, 7, 1, 9, 5, 2, 8, 4, 6, 8, 2, 6, 4, 9, 1, 5, 3, 7];
// Wrong reads the stub makes for every fifth calibration sample.
const CONFUSED = { 1: 7, 2: 7, 3: 5, 4: 9, 5: 3, 6: 5, 7: 1, 8: 3, 9: 4 };

// Unit-box digit strokes (x 0–0.75, y 0–1). A stroke is a list of pieces; "C" pieces are
// Catmull-Rom curves through their points, "L" pieces are straight polylines.
const FONT = {
    1: [[["C", [0.16, 0.22], [0.42, 0.0]], ["L", [0.42, 0.0], [0.4, 0.5], [0.37, 1.0]]]],
    2: [[["C", [0.04, 0.26], [0.2, 0.05], [0.42, 0.0], [0.62, 0.12], [0.6, 0.36], [0.36, 0.64], [0.02, 0.98]],
         ["C", [0.02, 0.98], [0.36, 0.93], [0.74, 0.97]]]],
    3: [[["C", [0.06, 0.12], [0.34, 0.0], [0.6, 0.1], [0.58, 0.3], [0.3, 0.46]],
         ["C", [0.3, 0.46], [0.62, 0.55], [0.68, 0.8], [0.45, 0.98], [0.15, 0.97], [0.0, 0.84]]]],
    4: [[["L", [0.5, 0.0], [0.03, 0.66], [0.76, 0.63]]], [["C", [0.56, 0.28], [0.54, 0.62], [0.5, 1.02]]]],
    5: [[["L", [0.68, 0.02], [0.18, 0.03], [0.12, 0.45]],
         ["C", [0.12, 0.45], [0.4, 0.36], [0.66, 0.48], [0.7, 0.75], [0.48, 0.97], [0.2, 0.98], [0.02, 0.86]]]],
    6: [[["C", [0.62, 0.0], [0.34, 0.18], [0.12, 0.5], [0.12, 0.82], [0.35, 1.0], [0.62, 0.9], [0.66, 0.65],
          [0.42, 0.52], [0.18, 0.62], [0.12, 0.76]]]],
    7: [[["L", [0.02, 0.05], [0.72, 0.03]], ["C", [0.72, 0.03], [0.48, 0.42], [0.36, 0.72], [0.3, 1.0]]]],
    8: [[["C", [0.6, 0.12], [0.38, 0.0], [0.12, 0.12], [0.15, 0.32], [0.45, 0.5], [0.7, 0.7], [0.6, 0.95],
          [0.32, 1.0], [0.1, 0.85], [0.15, 0.65], [0.45, 0.48], [0.62, 0.3], [0.6, 0.12]]]],
    9: [[["C", [0.66, 0.18], [0.42, 0.02], [0.15, 0.1], [0.12, 0.35], [0.38, 0.46], [0.62, 0.33], [0.66, 0.18]],
         ["L", [0.66, 0.18], [0.6, 0.6], [0.52, 1.0]]]]
};

let seed = 7;
let numberReads = 0;
function rand() { seed = (seed * 16807) % 2147483647; return seed / 2147483647; }
function jit(a) { return (rand() - 0.5) * 2 * a; }

function sampleCurve(p, step) {
    const out = [];
    for (let i = 0; i < p.length - 1; i++) {
        const p0 = p[Math.max(0, i - 1)], p1 = p[i], p2 = p[i + 1], p3 = p[Math.min(p.length - 1, i + 2)];
        const n = Math.max(2, Math.ceil(Math.hypot(p2[0] - p1[0], p2[1] - p1[1]) / step));
        for (let k = i === 0 ? 0 : 1; k <= n; k++) {
            const t = k / n, t2 = t * t, t3 = t2 * t;
            const f = j => 0.5 * (2 * p1[j] + (-p0[j] + p2[j]) * t + (2 * p0[j] - 5 * p1[j] + 4 * p2[j] - p3[j]) * t2
                                  + (-p0[j] + 3 * p1[j] - 3 * p2[j] + p3[j]) * t3);
            out.push([f(0), f(1)]);
        }
    }
    return out;
}
function sampleLine(p, step) {
    const out = [p[0]];
    for (let i = 0; i < p.length - 1; i++) {
        const n = Math.max(1, Math.ceil(Math.hypot(p[i + 1][0] - p[i][0], p[i + 1][1] - p[i][1]) / step));
        for (let k = 1; k <= n; k++)
            out.push([p[i][0] + (p[i + 1][0] - p[i][0]) * k / n, p[i][1] + (p[i + 1][1] - p[i][1]) * k / n]);
    }
    return out;
}

// Digit d written with glyph height h centred on (cx, cy): PROTOCOL strokes [x, y, t, p, …].
function write(d, cx, cy, h) {
    const rot = jit(5) * Math.PI / 180, cos = Math.cos(rot), sin = Math.sin(rot);
    const sx = h * (0.92 + jit(0.08)), slant = 0.12 + jit(0.05);
    return FONT[d].map(stroke => {
        let pts = [];
        for (const piece of stroke) {
            const kind = piece[0], ctl = piece.slice(1);
            const s = (kind === "C" ? sampleCurve : sampleLine)(ctl, 2.5 / h);
            pts = pts.concat(pts.length ? s.slice(1) : s);
        }
        const flat = [];
        let t = 0, px = null, py = null;
        for (const [ux, uy] of pts) {
            let x = (ux - 0.36) * sx, y = (uy - 0.5) * h;
            x -= slant * y;
            const X = cx + x * cos - y * sin + jit(0.3), Y = cy + x * sin + y * cos + jit(0.3);
            if (px !== null)
                t += Math.hypot(X - px, Y - py) / 0.3;
            flat.push(Math.round(X * 10) / 10, Math.round(Y * 10) / 10, Math.round(t), 1);
            px = X; py = Y;
        }
        return flat;
    });
}

function answerInk(cell, d) {
    const cx = (cell % 9 + 0.5) * S + jit(6), cy = (Math.floor(cell / 9) + 0.52) * S + jit(5);
    return write(d, cx, cy, S * (0.54 + jit(0.04))).map(p => ({ role: "answer", points: p }));
}
function candidateInk(cell, digits) {
    const x0 = (cell % 9) * S, y0 = Math.floor(cell / 9) * S;
    let out = [];
    for (const d of digits) {
        const cx = x0 + S * (0.2 + 0.3 * ((d - 1) % 3)) + jit(3), cy = y0 + S * (0.2 + 0.3 * Math.floor((d - 1) / 3)) + jit(3);
        out = out.concat(write(d, cx, cy, S * 0.23).map(p => ({ role: "candidate", points: p })));
    }
    return out;
}

function bits(digits) { return digits.reduce((m, d) => m | (1 << (d - 1)), 0); }

function conflicts(givens, entries) {
    const v = i => givens[i] !== "." ? givens[i] : entries[i];
    const out = new Set();
    for (let a = 0; a < 81; a++)
        for (let b = a + 1; b < 81; b++) {
            const ra = Math.floor(a / 9), ca = a % 9, rb = Math.floor(b / 9), cb = b % 9;
            const peers = ra === rb || ca === cb
                || (Math.floor(ra / 3) === Math.floor(rb / 3) && Math.floor(ca / 3) === Math.floor(cb / 3));
            if (peers && v(a) !== "." && v(a) === v(b)) { out.add(a); out.add(b); }
        }
    return [...out].sort((x, y) => x - y);
}

// Backtracking solver, for --solved and the completion check.
function solve(g) {
    const a = g.split("").map(ch => ch === "." ? 0 : Number(ch));
    function ok(i, d) {
        const r = Math.floor(i / 9), c = i % 9, br = r - r % 3, bc = c - c % 3;
        for (let k = 0; k < 9; k++)
            if (a[r * 9 + k] === d || a[k * 9 + c] === d || a[(br + Math.floor(k / 3)) * 9 + bc + k % 3] === d)
                return false;
        return true;
    }
    function go(i) {
        if (i === 81) return true;
        if (a[i]) return go(i + 1);
        for (let d = 1; d <= 9; d++)
            if (ok(i, d)) { a[i] = d; if (go(i + 1)) return true; a[i] = 0; }
        return false;
    }
    return go(0) ? a.join("") : "";
}
function isoDate(d) {
    const pad = n => (n < 10 ? "0" : "") + n;
    return d.getFullYear() + "-" + pad(d.getMonth() + 1) + "-" + pad(d.getDate());
}

let game = null;
let personalSamples = 31;
let calib = null;   // {done, total}

function played() {
    seed = 7;
    let entries = ENTRIES.split("");
    for (const c in EXTRA) entries[c] = String(EXTRA[c]);
    entries = entries.join("");
    const ink = {}, candidates = new Array(81).fill(0);
    for (let c = 0; c < 81; c++)
        if (entries[c] !== "." && c !== TYPED_ENTRY)
            ink[String(c)] = answerInk(c, Number(entries[c]));
    for (const c in PENCIL) {
        candidates[c] = bits(PENCIL[c]);
        ink[String(c)] = candidateInk(Number(c), PENCIL[c]);
    }
    for (const c in TYPED_CANDIDATES)
        candidates[c] = bits(TYPED_CANDIDATES[c]);
    return {
        puzzle: { number: 48213, seed: 48213, level: "hard", hardest: "Hidden pair", givens: GIVENS, daily: "" },
        entries: entries, candidates: candidates, conflicts: conflicts(GIVENS, entries), ink: ink,
        elapsedMs: 412000, solved: false, canUndo: true
    };
}

function fresh(level, daily) {
    const n = game.puzzle.number + 1;
    return {
        puzzle: { number: n, seed: n, level: level, hardest: "", givens: GIVENS, daily: daily || "" },
        entries: ".".repeat(81), candidates: new Array(81).fill(0), conflicts: [], ink: {},
        elapsedMs: 0, solved: false, canUndo: false
    };
}

function today() {
    const d = new Date();
    return { date: isoDate(d), level: DAILY_LEVELS[d.getDay()] };
}

// The whole `state`, with v2's stats and personalSamples.
function stateBody() {
    const stats = JSON.parse(JSON.stringify(STATS));
    const y = new Date();
    y.setDate(y.getDate() - 1);
    stats.daily.last = isoDate(y);
    return Object.assign({}, game, { stats: stats, personalSamples: personalSamples });
}

function solvedBody() {
    return { elapsedMs: 734000, mistakes: 1, hints: 2, best: true, streak: game.puzzle.daily ? 5 : 3 };
}

function msg(type, body) { return [type, JSON.stringify(body)]; }

// Replies to one frontend message: a list of [type, json].
function reply(type, contents, args) {
    const m = contents ? JSON.parse(contents) : {};
    if (!game) {
        game = played();
        if (args.indexOf("--first-run") >= 0)
            personalSamples = 0;
        if (args.indexOf("--daily") >= 0) {
            game.puzzle.daily = today().date;
            game.puzzle.level = today().level;
        }
        if (args.indexOf("--solved") >= 0) {
            game.entries = solve(GIVENS).split("").map((d, i) => GIVENS[i] === "." ? d : ".").join("");
            game.candidates = new Array(81).fill(0);
            game.conflicts = [];
            game.solved = true;
            game.ink = {};
            for (let c = 0; c < 81; c++)
                if (game.entries[c] !== ".")
                    game.ink[String(c)] = answerInk(c, Number(game.entries[c]));
        }
    }
    switch (type) {
    case 100: { // hello
        const out = [msg(1, stateBody())];
        if (args.indexOf("--hint") >= 0)
            out.push(msg(3, HINT));
        if (args.indexOf("--solved") >= 0)
            out.push(msg(6, solvedBody()));
        return out;
    }
    case 101: // newGame
        game = m.daily ? fresh(today().level, today().date) : fresh(m.level || "easy");
        if (m.number) {
            game.puzzle.number = game.puzzle.seed = m.number;
            game.puzzle.byNumber = true;
        }
        return [msg(1, stateBody())];
    case 113: // readInk: the stub can't read; boxes written in order read as 482137
        return [msg(10, { id: m.id, digit: Number("482137"[numberReads++ % 6]) })];
    case 102: { // ink: the stub can't read, so every mark comes back unknown
        const c = m.cell;
        return [msg(2, {
            id: m.id, cell: c,
            marks: [{ kind: "unknown", digit: 0, confidence: 0.4, top3: [], bbox: [0, 0, 0, 0],
                      strokes: m.strokes.map((_, i) => i) }],
            cellState: { entry: game.entries[c] === "." ? 0 : Number(game.entries[c]),
                         candidates: game.candidates[c], ink: game.ink[String(c)] || [] },
            conflicts: game.conflicts, unknown: true
        })];
    }
    case 103: { // setCell
        const e = game.entries.split("");
        e[m.cell] = m.digit ? String(m.digit) : ".";
        game.entries = e.join("");
        game.conflicts = conflicts(game.puzzle.givens, game.entries);
        game.canUndo = true;
        const full = game.entries.split("").map((d, i) => GIVENS[i] !== "." ? GIVENS[i] : d).join("");
        if (!game.solved && full === solve(GIVENS)) {
            game.solved = true;
            return [msg(1, stateBody()), msg(6, solvedBody())];
        }
        return [msg(1, stateBody())];
    }
    case 104: // toggleCandidate
        game.candidates[m.cell] ^= 1 << (m.digit - 1);
        game.canUndo = true;
        return [msg(1, stateBody())];
    case 105: // hint: the same step for {reveal: false} (a nudge) and {reveal: true}
        return [msg(3, HINT)];
    case 106:
        return [msg(1, stateBody())];
    case 109: // calibrationStart
        calib = { done: 0, total: 27 };
        return [msg(7, { total: 27, done: 0, next: 1, samples: personalSamples })];
    case 110: { // calibrationInk: every fifth sample is misread
        if (!calib)
            return [msg(5, { id: m.id, message: "Calibration hasn't started." })];
        calib.done = Math.min(calib.done + 1, calib.total);
        personalSamples++;
        const wrong = calib.done % 5 === 0;
        const out = [msg(8, { id: m.id, digit: m.digit, read: wrong ? CONFUSED[m.digit] : m.digit,
                               confidence: wrong ? 0.48 : 0.93, done: calib.done, total: calib.total })];
        const next = calib.done >= calib.total ? 0 : CALIB_ORDER[calib.done];
        out.push(msg(7, { total: calib.total, done: calib.done, next: next, samples: personalSamples }));
        return out;
    }
    case 111: // calibrationDone
        calib = null;
        return [msg(9, { samples: personalSamples, before: 0.89, after: 0.98 }), msg(1, stateBody())];
    }
    return [];
}
