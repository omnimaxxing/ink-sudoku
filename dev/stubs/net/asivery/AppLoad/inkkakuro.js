// Canned Ink Kakuro backend for the Mac stub: answers the frontend's messages with
// KAKURO.md-shaped replies. The puzzles are hand-picked layouts (one per level size)
// filled with distinct digits per run by a seeded search; the clues are the fill's run
// sums, so every puzzle is a valid Kakuro (checked at load; problems are logged).
// Handwriting comes from inksudoku.js's single-stroke digit font.
// Flags (after `--` on the qml6 command line):
//   --level L    the restored game is level L (easy 7×7, medium 9×9 (default), hard 11×11, expert 13×13)
//   --hint       also send a hint after `state`
//   --hint-lone  hints are a single-cell placement (default: a run with one way to make its sum)
//   --daily      the restored game is today's daily puzzle (its level follows the weekday unless --level)
//   --solved     the restored game is solved, and `solved` follows `state`
//   --first-run  personalSamples is 0 (the frontend shows its first-run line)
//   --blank      the restored game has no entries yet
//   --preparing  no saved game and the first puzzle isn't ready: `hello` gets no reply
// `ink` replies `marks` with every stroke unknown (the stub can't read), or `error` with
// the id when the cell is a clue cell; setCell/toggleCandidate there reply `error` without
// one (as the server does). A setCell that completes the grid sends `solved`.
.pragma library
.import "inksudoku.js" as InkSudoku

const GRID = 1180;
const LEVEL_N = { easy: 7, medium: 9, hard: 11, expert: 13 };
const LAYOUTS = {
    7: ["#######",
        "#......",
        "#......",
        "##...##",
        "###...#",
        "#......",
        "#......"],
    9: ["#########",
        "#...#....",
        "#...#....",
        "###......",
        "#....##..",
        "#..##....",
        "#......##",
        "#....#...",
        "#....#..."],
    11: ["###########",
         "#..##..#...",
         "#...#..#...",
         "#......#...",
         "###........",
         "###..#....#",
         "##....#..##",
         "#........##",
         "#...#......",
         "#...#..#...",
         "#...#..##.."],
    13: ["#############",
         "##..#.......#",
         "#...#........",
         "#......#.....",
         "#......#..#..",
         "#.........###",
         "#...#...##..#",
         "##..##...#...",
         "####.........",
         "#..#..#......",
         "#.....#......",
         "#........#...",
         "##.......#..#"]
};
// The 9×9 game in progress: answers written so far (r, c), one wrong answer that repeats a
// digit in its run, pencil marks, and an answer + marks typed through the no-ink fallback.
const PLAYED9 = {
    answers: [[1, 1], [1, 2], [1, 3], [2, 1], [2, 2], [2, 3], [3, 3], [3, 4], [1, 5], [2, 5],
              [5, 1], [6, 1], [7, 1], [8, 1], [6, 2], [7, 2]],
    wrong: [[8, 2]],                       // repeats a digit of its down run (column 2)
    pencil: [[3, 5], [4, 3], [7, 7], [8, 7]],
    typed: [[4, 2]],
    typedPencil: [[6, 4]]
};
const STATS = {
    easy: { played: 4, solved: 3, bestMs: 251000, streak: 3 },
    medium: { played: 6, solved: 4, bestMs: 618000, streak: 1 },
    hard: { played: 2, solved: 1, bestMs: 1342000, streak: 0 },
    expert: { played: 0, solved: 0, bestMs: 0, streak: 0 },
    daily: { streak: 3, last: "" }
};
// Fill seeds picked for many runs with a single way to make their sum, as real puzzles have.
const FILL_SEEDS = { 7: 1531, 9: 132, 11: 1084, 13: 1338 };
const DAILY_LEVELS = ["expert", "easy", "easy", "medium", "medium", "hard", "hard"];

let seed = 11;
function rand() { seed = (seed * 16807) % 2147483647; return seed / 2147483647; }

// Runs of a layout: [{cells, clue: black cell index, dir: "a" | "d"}].
function runsOf(layout) {
    const n = layout.length, out = [];
    const white = (r, c) => r < n && c < n && layout[r][c] === ".";
    for (let r = 0; r < n; r++)
        for (let c = 0; c < n; c++) {
            if (white(r, c))
                continue;
            if (white(r, c + 1)) {
                const cells = [];
                for (let k = c + 1; white(r, k); k++)
                    cells.push(r * n + k);
                out.push({ cells: cells, clue: r * n + c, dir: "a" });
            }
            if (white(r + 1, c)) {
                const cells = [];
                for (let k = r + 1; white(k, c); k++)
                    cells.push(k * n + c);
                out.push({ cells: cells, clue: r * n + c, dir: "d" });
            }
        }
    return out;
}

// A seeded fill with distinct digits in every run (backtracking).
function fill(layout, fillSeed) {
    const n = layout.length, runs = runsOf(layout), sol = new Array(n * n).fill(0);
    const peers = {};
    for (const run of runs)
        for (const c of run.cells)
            peers[c] = (peers[c] || []).concat(run.cells.filter(x => x !== c));
    const whites = Object.keys(peers).map(Number).sort((a, b) => a - b);
    seed = fillSeed;
    function go(i) {
        if (i === whites.length)
            return true;
        const c = whites[i], digits = [1, 2, 3, 4, 5, 6, 7, 8, 9];
        for (let k = 8; k > 0; k--) {
            const j = Math.floor(rand() * (k + 1)), t = digits[k];
            digits[k] = digits[j];
            digits[j] = t;
        }
        for (const d of digits) {
            if (peers[c].some(p => sol[p] === d))
                continue;
            sol[c] = d;
            if (go(i + 1))
                return true;
        }
        sol[c] = 0;
        return false;
    }
    go(0);
    return sol;
}

function makePuzzle(level, number, daily) {
    const n = LEVEL_N[level] || 9, layout = LAYOUTS[n], runs = runsOf(layout);
    const solution = fill(layout, FILL_SEEDS[n]);
    const cells = [];
    for (let i = 0; i < n * n; i++)
        cells.push(layout[Math.floor(i / n)][i % n] === "#" ? { b: 1, d: 0, a: 0 } : { b: 0 });
    for (const run of runs)
        cells[run.clue][run.dir] = run.cells.reduce((s, c) => s + solution[c], 0);
    check(n, cells, solution, runs);
    return {
        puzzle: { number: number, seed: number, level: level, hardest: "Unique sum", daily: daily || "", n: n, cells: cells },
        solution: solution, runs: runs
    };
}

// The canned puzzle must be a valid Kakuro: say so loudly if it isn't.
function check(n, cells, solution, runs) {
    const bad = [];
    for (let i = 0; i < n; i++)
        if (!cells[i].b || !cells[i * n].b)
            bad.push("row 0 / column 0 not black at " + i);
    for (const run of runs) {
        const ds = run.cells.map(c => solution[c]);
        if (run.cells.length < 2 || run.cells.length > 9)
            bad.push("run of " + run.cells.length + " at " + run.clue);
        if (new Set(ds).size !== ds.length || ds.some(d => d < 1 || d > 9))
            bad.push("repeated or missing digit in run at " + run.clue + run.dir + ": " + ds);
        if (cells[run.clue][run.dir] !== ds.reduce((s, d) => s + d, 0))
            bad.push("clue " + run.clue + run.dir + " doesn't match its run");
    }
    for (let i = 0; i < n * n; i++)
        if (cells[i].b && solution[i] || !cells[i].b && !solution[i])
            bad.push("cell " + i + ": solution doesn't match the layout");
    if (bad.length)
        console.log("inkkakuro: INVALID canned puzzle " + n + "×" + n + ": " + bad.join("; "));
}

function cellSize(n) { return GRID / n; }

function answerInk(n, cell, d) {
    const S = cellSize(n);
    const cx = (cell % n + 0.5) * S + (rand() - 0.5) * S * 0.08, cy = (Math.floor(cell / n) + 0.52) * S + (rand() - 0.5) * S * 0.06;
    return InkSudoku.write(d, cx, cy, S * (0.54 + (rand() - 0.5) * 0.06)).map(p => ({ role: "answer", digit: d, points: p }));
}
function candidateInk(n, cell, digits) {
    const S = cellSize(n), x0 = (cell % n) * S, y0 = Math.floor(cell / n) * S;
    let out = [];
    for (const d of digits) {
        const cx = x0 + S * (0.2 + 0.3 * ((d - 1) % 3)) + (rand() - 0.5) * 5, cy = y0 + S * (0.2 + 0.3 * Math.floor((d - 1) / 3)) + (rand() - 0.5) * 5;
        out = out.concat(InkSudoku.write(d, cx, cy, S * 0.23).map(p => ({ role: "candidate", digit: d, points: p })));
    }
    return out;
}
function bits(digits) { return digits.reduce((m, d) => m | (1 << (d - 1)), 0); }

// Check: repeated digits in a run, or a full run with the wrong sum.
function conflicts(g) {
    const out = new Set();
    for (const run of g.runs) {
        const ds = run.cells.map(c => g.entries[c]);
        for (let i = 0; i < ds.length; i++)
            for (let j = i + 1; j < ds.length; j++)
                if (ds[i] && ds[i] === ds[j]) {
                    out.add(run.cells[i]);
                    out.add(run.cells[j]);
                }
        if (ds.every(d => d) && ds.reduce((s, d) => s + d, 0) !== g.puzzle.cells[run.clue][run.dir])
            for (const c of run.cells)
                out.add(c);
    }
    return [...out].sort((a, b) => a - b);
}

// A plausible pencil mark: the right digit and one or two others not yet in its runs.
function marksFor(g, c) {
    const used = new Set();
    for (const run of g.runs)
        if (run.cells.indexOf(c) >= 0)
            for (const x of run.cells)
                if (g.entries[x])
                    used.add(g.entries[x]);
    const out = [g.solution[c]];
    for (let d = 1; d <= 9 && out.length < 3; d++)
        if (d !== g.solution[c] && !used.has(d) && (d + c) % 3 !== 0)
            out.push(d);
    return out.sort((a, b) => a - b);
}

function played(level, blank) {
    const g = makePuzzle(level, 31337, "");
    const n = g.puzzle.n;
    g.entries = new Array(n * n).fill(0);
    g.candidates = new Array(n * n).fill(0);
    g.ink = {};
    g.elapsedMs = 497000;
    g.solved = false;
    g.canUndo = !blank;
    if (blank)
        return g;
    const at = (rc) => rc[0] * n + rc[1];
    let plan = PLAYED9;
    if (n !== 9) {      // other sizes: a scattering, by a fixed rule
        const whites = [];
        for (let c = 0; c < n * n; c++)
            if (!g.puzzle.cells[c].b)
                whites.push([Math.floor(c / n), c % n]);
        plan = {
            answers: whites.filter(rc => (rc[0] * 5 + rc[1] * 3) % 7 < 3 && rc[0] < n - 3),
            wrong: [],
            pencil: whites.filter(rc => (rc[0] * 5 + rc[1] * 3) % 7 === 4).slice(0, 8),
            typed: [],
            typedPencil: []
        };
    }
    for (const rc of plan.answers.concat(plan.typed))
        g.entries[at(rc)] = g.solution[at(rc)];
    for (const rc of plan.wrong) {
        // a digit already elsewhere in the same down run
        const c = at(rc), run = g.runs.find(r => r.dir === "d" && r.cells.indexOf(c) >= 0);
        g.entries[c] = g.entries[run.cells.find(x => x !== c && g.entries[x])];
    }
    seed = 7;
    for (const rc of plan.answers.concat(plan.wrong))
        g.ink[String(at(rc))] = answerInk(n, at(rc), g.entries[at(rc)]);
    for (const rc of plan.pencil) {
        const c = at(rc), ds = marksFor(g, c);
        g.candidates[c] = bits(ds);
        g.ink[String(c)] = candidateInk(n, c, ds);
    }
    for (const rc of plan.typedPencil)
        g.candidates[at(rc)] = bits(marksFor(g, at(rc)));
    return g;
}

// A hint the engine could give: a run, not yet full, whose sum can only be made one way.
function combos(len, sum) {
    const out = [];
    for (let m = 0; m < 512; m++) {
        const ds = [];
        for (let d = 1; d <= 9; d++)
            if (m & (1 << (d - 1)))
                ds.push(d);
        if (ds.length === len && ds.reduce((s, d) => s + d, 0) === sum)
            out.push(ds);
    }
    return out;
}
const WORDS = ["", "", "two", "three", "four", "five", "six", "seven", "eight", "nine"];
function hint(g, lone) {
    if (lone) {     // a single placement, for a one-cell loop
        const c = g.runs.find(r => r.dir === "a" && r.cells.some(x => !g.entries[x])).cells.find(x => !g.entries[x]);
        const r = Math.floor(c / g.puzzle.n), col = c % g.puzzle.n, d = g.solution[c];
        return {
            technique: "Last digit", cells: [c], digits: [d], placement: { cell: c, digit: d },
            title: "The " + d + " finishes it",
            text: "Row " + r + ", column " + col + " is the last square its runs leave open: only a " + d + " makes both sums."
        };
    }
    const n = g.puzzle.n, empty = run => run.cells.filter(c => !g.entries[c]).length;
    const runs = g.runs.slice().sort((x, y) => empty(y) - empty(x));
    for (const run of runs) {
        const sum = g.puzzle.cells[run.clue][run.dir], all = combos(run.cells.length, sum);
        if (all.length !== 1 || !empty(run) || run.cells.length > 4)
            continue;
        const combo = all[0], r0 = Math.floor(run.cells[0] / n), c0 = run.cells[0] % n;
        const last = run.cells[run.cells.length - 1];
        const where = run.dir === "a"
            ? "The across run in row " + r0 + ", columns " + c0 + " to " + (last % n)
            : "The down run in column " + c0 + ", rows " + r0 + " to " + Math.floor(last / n);
        const ds = combo.slice(0, -1).join(", ") + " and " + combo[combo.length - 1];
        return {
            technique: "Unique sum", cells: run.cells, digits: combo,
            title: "Only one way to make " + sum + " in " + WORDS[run.cells.length],
            text: where + " adds up to " + sum + " with " + WORDS[run.cells.length]
                  + " different digits. That can only be " + ds + ", so no other digit goes in these squares."
        };
    }
    return { technique: "Unique sum", title: "", text: "Every run with a single way to make its sum is filled in." };
}

function isoDate(d) {
    const pad = k => (k < 10 ? "0" : "") + k;
    return d.getFullYear() + "-" + pad(d.getMonth() + 1) + "-" + pad(d.getDate());
}
function today() {
    const d = new Date();
    return { date: isoDate(d), level: DAILY_LEVELS[d.getDay()] };
}

let game = null;
let personalSamples = 58;

function stateBody() {
    const stats = JSON.parse(JSON.stringify(STATS));
    const y = new Date();
    y.setDate(y.getDate() - 1);
    stats.daily.last = isoDate(y);
    return {
        puzzle: game.puzzle, entries: game.entries, candidates: game.candidates, conflicts: conflicts(game),
        ink: game.ink, elapsedMs: game.elapsedMs, solved: game.solved, canUndo: game.canUndo,
        stats: stats, personalSamples: personalSamples
    };
}
function solvedBody() {
    return { elapsedMs: 1123000, mistakes: 1, hints: 1, best: false, streak: game.puzzle.daily ? 4 : 2 };
}
function msg(type, body) { return [type, JSON.stringify(body)]; }
function flag(args, f) { return args.indexOf(f) >= 0; }
function opt(args, f) { const i = args.indexOf(f); return i >= 0 ? args[i + 1] : ""; }

function reply(type, contents, args) {
    const m = contents ? JSON.parse(contents) : {};
    if (!game) {
        const lv = opt(args, "--level");
        const level = LEVEL_N[lv] ? lv : flag(args, "--daily") ? today().level : "medium";
        game = played(level, flag(args, "--blank"));
        if (flag(args, "--first-run"))
            personalSamples = 0;
        if (flag(args, "--daily")) {
            game.puzzle.daily = today().date;
            game.puzzle.number = game.puzzle.seed = Number(today().date.replace(/-/g, ""));
        }
        if (flag(args, "--solved")) {
            const n = game.puzzle.n;
            game.entries = game.solution.slice();
            game.candidates = new Array(n * n).fill(0);
            game.solved = true;
            game.ink = {};
            seed = 9;
            for (let c = 0; c < n * n; c++)
                if (game.entries[c])
                    game.ink[String(c)] = answerInk(n, c, game.entries[c]);
        }
    }
    const n = game.puzzle.n;
    switch (type) {
    case 100: { // hello
        if (flag(args, "--preparing"))
            return [];
        const out = [msg(1, stateBody())];
        if (flag(args, "--hint"))
            out.push(msg(3, hint(game, flag(args, "--hint-lone"))));
        if (flag(args, "--solved"))
            out.push(msg(6, solvedBody()));
        return out;
    }
    case 101: { // newGame
        const level = m.daily ? today().level : LEVEL_N[m.level] ? m.level : "easy";
        const num = m.daily ? Number(today().date.replace(/-/g, "")) : game.puzzle.number + 1;
        const g = makePuzzle(level, num, m.daily ? today().date : "");
        g.entries = new Array(g.puzzle.n * g.puzzle.n).fill(0);
        g.candidates = new Array(g.puzzle.n * g.puzzle.n).fill(0);
        g.ink = {};
        g.elapsedMs = 0;
        g.solved = false;
        g.canUndo = false;
        game = g;
        return [msg(1, stateBody())];
    }
    case 102: { // ink
        const c = m.cell;
        if (!(c >= 0 && c < n * n) || game.puzzle.cells[c].b)
            return [msg(5, { id: m.id, message: "ink: cell " + c + " is a clue cell" })];
        return [msg(2, {
            id: m.id, cell: c,
            marks: [{ kind: "unknown", digit: 0, confidence: 0.4, top3: [], bbox: [0, 0, 0, 0], erases: [],
                      strokes: m.strokes.map((_, i) => i) }],
            cellState: { entry: game.entries[c], candidates: game.candidates[c], ink: game.ink[String(c)] || [] },
            conflicts: conflicts(game), unknown: true, canUndo: game.canUndo
        })];
    }
    case 103: { // setCell
        if (!game.puzzle.cells[m.cell] || game.puzzle.cells[m.cell].b)
            return [msg(5, { message: "setCell: cell " + m.cell + " is a clue cell" })];
        game.entries[m.cell] = m.digit || 0;
        delete game.ink[String(m.cell)];
        game.canUndo = true;
        if (!game.solved && game.entries.every((d, i) => d === game.solution[i])) {
            game.solved = true;
            return [msg(1, stateBody()), msg(6, solvedBody())];
        }
        return [msg(1, stateBody())];
    }
    case 104: // toggleCandidate
        if (!game.puzzle.cells[m.cell] || game.puzzle.cells[m.cell].b)
            return [msg(5, { message: "toggleCandidate: cell " + m.cell + " is a clue cell" })];
        game.candidates[m.cell] ^= 1 << (m.digit - 1);
        game.canUndo = true;
        return [msg(1, stateBody())];
    case 105: // hint: the same step for a nudge and a reveal
        if (game.solved)
            return [msg(5, { message: "This one is solved already." })];
        return [msg(3, hint(game, flag(args, "--hint-lone")))];
    case 106: // undo
        return [msg(1, stateBody())];
    }
    return [];
}

// For dev/shot.qml: white cells of the canned game that are still empty.
function emptyCells() {
    if (!game)
        return [];
    const out = [];
    for (let c = 0; c < game.entries.length; c++)
        if (!game.puzzle.cells[c].b && !game.entries[c] && !game.ink[String(c)])
            out.push(c);
    return out;
}
