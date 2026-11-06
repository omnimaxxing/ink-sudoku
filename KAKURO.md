# Ink Kakuro: design + contract

Second pen game, built on the Ink Sudoku stack (see `README.md`, `PROTOCOL.md`). Same principles:
pen-only, your handwriting stays on the grid, infinite procedural puzzles with exactly one solution,
graded by the human techniques they need, hints that teach in plain words, paper/book look.

## Rules
A square grid of black clue cells and white cells. A *run* is a maximal horizontal (across) or
vertical (down) line of white cells. The clue cell left of an across run holds its sum (top-right
number); the clue cell above a down run holds its sum (bottom-left number). Each run uses distinct
digits 1–9 adding up to its clue. Runs are 2–9 cells (no single-cell runs).

## Layout
| Package / folder | Owner | What |
|---|---|---|
| `backend/kakuro/` | engine agent | grid model, solver (counts solutions), generator, human-technique grader, `NextStep` hints, `Check` |
| `backend/cmd/kakurogen/` | engine agent | CLI: `-level -seed -n -q`, prints puzzles + timing/technique stats |
| `backend/kgame/`, `backend/cmd/kakuro/` | server agent | game state, persistence, stats/daily, AppLoad protocol server |
| `kakuro-app/` | frontend agent | QML app, manifest, icon, `build.sh` → `dist/ink-kakuro/` |

Reuse, don't fork: `appload`, `pen`, `hwr` (incl. `Analyzer`, `Personal`) are shared. QML components
(`Writer`, `InkLayer`, `SettledInk`, `QuietRefresh`, `Action`) may be copied from `app/ui/` and adapted
(they're grid-size-aware already via `divisions`).

## Engine API (`backend/kakuro`, contract — a stub with these signatures exists)
```go
type Level int // Easy, Medium, Hard, Expert; String() "easy"…; ParseLevel(string) (Level, error)
type Cell struct {
    Black  bool
    Down   uint8 // clue for the down run below this black cell, 0 = none
    Across uint8 // clue for the across run right of this black cell, 0 = none
}
type Puzzle struct {
    N          int     // grid is N×N, cells row-major, index = r*N + c; row 0 and column 0 are always black
    Cells      []Cell
    Solution   []uint8 // 0 on black cells
    Seed       uint64
    Level      Level
    Hardest    string         // hardest technique needed (plain name)
    Techniques map[string]int // technique -> times used in the logical solve
}
func Generate(seed uint64, level Level) Puzzle       // deterministic; unique solution; within the level's technique set
func Check(p Puzzle, entries []uint8) []int          // cells in conflict: repeated digit in a run, or a full run with the wrong sum
func AutoCandidates(p Puzzle, entries []uint8) []uint16 // bit d-1 = digit d possible, from run sums/combinations and entries
func NextStep(p Puzzle, entries []uint8, candidates []uint16) (Step, bool)
type Step struct { // JSON like engine.Step: numeric arrays, plain-language Title/Text
    Technique    string `json:"technique"`
    Title        string `json:"title"`
    Text         string `json:"text"`
    Cells        []int  `json:"cells,omitempty"`
    Digits       []int  `json:"digits,omitempty"`
    Placement    *Cand  `json:"placement,omitempty"`
    Eliminations []Cand `json:"eliminations,omitempty"`
}
type Cand struct { Cell int `json:"cell"`; Digit int `json:"digit"` }
```
Levels: Easy N=7, Medium N=9, Hard N=11, Expert N=13 (N includes the clue row/column; white cell size
= 1180/N px, so Expert cells are ~91 px — still comfortable for answers; pencil marks get small).

## Protocol (frontend ↔ `cmd/kakuro`)
Same transport and message types as `PROTOCOL.md` (state 1, marks 2, hint 3, pen 4, error 5, solved 6;
hello 100, newGame 101, ink 102, setCell 103, toggleCandidate 104, hint 105 with `reveal`, undo 106,
settings 107, log 108), with these differences:
- `state.puzzle`: `{"number", "seed", "level", "hardest", "daily", "n": 9, "cells": [{"b":1,"d":17,"a":0}, {"b":0}, …]}`
  (`b` black flag, `d` down clue, `a` across clue; white cells are `{"b":0}`).
- `state.entries`: array of `n*n` ints (0 = empty / black), not an 81-char string. `candidates`: `n*n` ints.
- Cell indices everywhere are `r*n + c`; ink coordinates are grid-local px with cell size `gridSize / n`.
- `ink` on a black cell is rejected with an `error` carrying the `id`.
- No calibration messages (7/8/9, 109–111): Kakuro **reads the player's handwriting samples from
  Ink Sudoku** (`/home/root/.local/share/ink-sudoku/calibration.jsonl` and `samples.jsonl`) into its
  `hwr.Personal`, and appends its own corrections to `/home/root/.local/share/ink-kakuro/samples.jsonl`.
  `state.personalSamples` reports the total.
- Daily: same seed-from-date rule, level by weekday (Mon/Tue easy … Sun expert), pre-generated at startup.
- Persistence: `/home/root/.local/share/ink-kakuro/` (`state.json`, `stats.json`, `samples.jsonl`, `backend.log`).

App: id `ink-kakuro`, name "Ink Kakuro".

## Server notes
Decisions `backend/cmd/kakuro` and `backend/kgame` made where the above is silent. PROTOCOL.md's
Backend notes (v1 and v2) still hold unless changed here.

- **`state.puzzle.cells`**: a white cell is exactly `{"b":0}`; a clue cell always carries both clues,
  0 for none: `{"b":1,"d":17,"a":0}`. `number` = `seed` (random 1–999999, or the daily's `YYYYMMDD`).
  A random seed can come up again at another level, so a state is a new game when `number` *or*
  `level` differs.
- **`entries`** and **`candidates`** are always `n*n` ints; clue cells are 0 in both.
- **Clue cells**: `ink` there replies `error` with the message's `id` (`"ink: cell 3 is a clue cell"`)
  and changes nothing; so does a cell out of range, a malformed stroke, or `ink` before there is a
  game. `setCell` / `toggleCandidate` on a clue cell reply `error` without `id` (there are no givens
  to ignore).
- **`conflicts`** is `kakuro.Check` (a digit repeated in a run, or a complete run with the wrong
  sum): white cells only, sorted, never `null`.
- **`hint`** is `kakuro.Step` JSON: `technique`, `title`, `text`, and when set `cells`, `digits`,
  `placement`, `eliminations` (no `houses`). If the engine has no step on an unsolved board, the
  backend makes one up: `technique: "Mistake"` with the wrong entries in `cells` (and the digit in
  `digits` when there's one), else `"Reveal"` with a `placement` for the first empty cell (the text
  names cells "row r, column c" with r, c the grid indices, so the first white row is row 1). On a
  solved board `hint` replies `error`. `reveal: false` is a nudge and isn't counted; `true` or absent
  counts toward `solved.hints` (as in PROTOCOL.md).
- **`hello` never waits for the generator.** A restored game is answered at once (`state.json` keeps
  the whole puzzle; nothing is regenerated). With no saved game, the first (easy) puzzle comes from
  the pre-generation queue; if it isn't ready yet, that `hello` (and any repeat meanwhile) gets no
  reply and the `state` follows on its own as soon as the puzzle is. The frontend's `hello` repeat
  already copes; it just has to take a `state` it didn't ask for right then.
- **`newGame`** takes the level's pre-generated puzzle (one per level, refilled in the background,
  easy first). If it isn't ready the reply waits for the one being generated (a few hundred ms at
  worst for expert on the tablet). `{"seed": N}` generates on the spot. Today's daily is generated
  at startup; `newGame {"daily": true}` before it's done waits for it. A generator failure replies
  `error` and the backend keeps running.
- **Handwriting samples.** At startup the backend reads Ink Sudoku's `calibration.jsonl` and
  `samples.jsonl` (`$INK_SUDOKU_DIR`, default `/home/root/.local/share/ink-sudoku/`), then its own
  `samples.jsonl`, into one `hwr.Personal`, built in the background (the plain model reads ink until
  it's ready). Corrections (Sudoku's rules: same cell, 20 s, unsure read or written over) are appended
  to Kakuro's `samples.jsonl` only and learned live; nothing is ever written to Ink Sudoku's dir.
  `personalSamples` counts both (`hwr.Personal.Len()`, so at most 24 per digit). Ink Sudoku doesn't
  read Kakuro's corrections.
- **Calibration messages** (109–111) get `error` "unknown message type"; the frontend shouldn't send them.
- **Files** in `$INK_KAKURO_DIR` (default `/home/root/.local/share/ink-kakuro/`): `state.json`
  (format `v: 1`), `stats.json`, `samples.jsonl`, `backend.log` (rotated at 1 MB). A `state.json`
  that doesn't parse or has another `v` is moved to `state.json.bad` and `hello` starts a new easy game.
- **Same as Sudoku:** the timer (each gap between frontend messages adds at most 90 s; stops while
  solved), stats and streaks, undo (200 deep, saved), `gridSize` rescaling of stored ink, `marks`
  shape and stroke indexing, `pen`/`settings`, `log`. Ink coordinates use cell size `gridSize / n`;
  hwr's size rules are relative to the cell (an answer is at least 40% of the cell tall, a pencil mark
  at most 33%), so they hold at every level.
- **Fixtures**: `INK_KAKURO_FIXTURES=$PWD/../dev/fixtures-kakuro pixi run go test ./cmd/kakuro -run
  Fixtures` (from `backend/`) records a real session to `session.json` / `session.js`; it skips while
  `kakuro.Generate` is still the stub.

## Frontend notes
Decisions the QML side (`kakuro-app/`) made where the above is silent. PROTOCOL.md's Frontend notes
(v1 and v2, minus calibration) still hold unless changed here.

- **Grid size** is `puzzle.n` (else √`cells.length`; 9 before the first `state`). The board,
  `Writer.divisions`, settled ink and every cell index use it; text and marks scale with the cell
  (`1180 / n` px). Pencil marks and typeset stand-ins keep Ink Sudoku's 3×3 spots (0.2/0.5/0.8 of the
  cell) at every size.
- **New game** = `number` or `level` differs (as in the Server notes): live and dimmed ink go, the
  page flashes.
- **Before the first `state`** the grid is hidden and takes no ink, and the tip line reads "Preparing
  a puzzle…" ("No answer from the game yet." after 30 unanswered `hello`s, one a second). A `state` is
  taken whenever it comes, asked for or not.
- **Clue cells never take ink.** A stroke that starts in a clue cell within a quarter cell of a white
  cell belongs to that white cell (the unsent group's cell first), so a digit begun just over the line
  stays whole and `ink.cell` is always white; strokes starting deeper are dropped like strokes off the
  grid (nothing drawn or sent). A clue-cell `error` with `id` is then handled like any other (that ink
  is erased, the message shown). The frontend never sends `setCell`/`toggleCandidate`. Taps and finger
  selection only select white cells; a pen tap in a clue cell still dismisses a visible hint. Types 7/8/9 are ignored; 109–111 are never sent.
- **Clue cells are a halftone, not a gray.** The live ink's UFast region covers the whole grid, so the
  board can be redrawn in 1-bit modes (see `app/ui/Board.qml`); a flat gray would break up there. Clue
  cells use Qt's 8×8 `Dense6Pattern` (12% black dots, a light gray at 229 ppi), cells with no clue the
  denser `Dense5Pattern` and no diagonal. A 2 px diagonal splits each clue cell: the down sum is
  centred at (0.30, 0.72) of the cell, the across sum at (0.72, 0.30), in EB Garamond Medium lining
  figures (the weight Ink Sudoku's givens already use on the device) at `12 + 0.27·cell` px (37 px at 13×13, 47 at 9×9, at most 54), with a 1 px white outline
  so they stay clean on the dots. Lines 2 px, frame 6 px, no boxes.
- **Conflicts**: Ink Sudoku's inset outline scaled to the cell (2 px under 110 px cells), white cells
  only (a clue cell in `conflicts` is ignored).
- **Hints**: title, then text, as in Ink Sudoku. `cells` are grouped into runs (horizontal stretches
  first, then vertical), and each run gets one hand-drawn rounded loop inside its cells (clear of the
  grid lines and the neighbouring sums); a lone cell gets an ellipse. `digits`, `placement` and
  `eliminations` aren't drawn.
- **Handwriting** comes from Ink Sudoku; there is no calibration page. On a first run
  (`personalSamples` 0 in the session's first `state`) the tip line reads "Teach Ink Sudoku your hand
  and Kakuro reads it too." (not tappable) until the player writes. The New game list ends with a
  quiet line: "Reads your hand from N samples taught in Ink Sudoku.", or that first-run sentence at 0.
- **Running head** "Kakuro No. N", or "Kakuro for Sunday, 1 November" for a daily; level on the right.
  The New game list is Ink Sudoku's (Today's puzzle with the same weekday table, then the four levels
  with best/streak), the solved page is Ink Sudoku's unchanged, and Refresh, ghosting and the
  pen-stream / finger-writes toggles work as there.
- **Mac dev**: the AppLoad stub answers app id `ink-kakuro` from `dev/stubs/net/asivery/AppLoad/inkkakuro.js`
  (valid hand-picked puzzles for all four sizes, checked at load); flags are in its header and
  `dev/shot.qml`'s, e.g. `--level expert --hint-press 2`, `--chooser`, `--solved`, `--first-run`,
  `--preparing`, `--ink-edge`.
