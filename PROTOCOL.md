# Ink Sudoku: frontend ↔ backend protocol

The QML frontend and the Go backend (`backend/cmd/sudoku`) talk over AppLoad's socket
(`backend/appload`): each message is an integer type plus a UTF-8 JSON body. The backend owns all game
state (puzzle, entries, candidates, the player's ink, undo history, timer) and persists it; the
frontend renders and captures ink. Either side must ignore unknown fields.

## Conventions

- Cells are 0–80, row-major (`r = cell / 9`, `c = cell % 9`). Houses: rows 0–8, columns 9–17, boxes
  18–26 (as in `engine`).
- Digit strings are 81 chars, `.` for empty (`engine.Grid.String()` format).
- Candidates are an array of 81 ints, bit `d-1` = digit `d` (`engine.CandidateBit`).
- **Ink coordinates are grid-local pixels**: origin at the grid's top-left corner, `gridSize` px
  square, so cell `c` spans `[(c%9)·s, (c/9)·s, s, s]` with `s = gridSize / 9`. A stroke is a flat
  array of points `[x, y, t, p, x, y, t, p, …]`: `t` = ms since the stroke started, `p` = pressure 0–1
  (send 1 when unknown, e.g. from mouse events).
- Levels: `"easy" | "medium" | "hard" | "expert"`.
- **Big messages come in parts (type 50, both games).** The kernel refuses a socket packet bigger
  than the sender's buffer (208 KiB on the tablet), and a finished game's `state` with all its ink
  is bigger (a full easy game: 264 KB). The backend sends any body over 64 KiB as type-50 messages
  whose bodies are `"<type> <index> <count>\n"` + the next slice of the original; the frontend
  joins slices 0..count-1 and handles the result as one message of `<type>` (`appload.Parts`,
  `joinPart` in each `main.qml`). Slices never cut a UTF-8 character.

## Backend → frontend

| Type | Name | Body |
|---|---|---|
| 1 | `state` | the whole game, sent after `hello`, `newGame`, `undo`, `setCell`, `toggleCandidate` |
| 2 | `marks` | recognition result for one `ink` message, plus the cell's new state |
| 3 | `hint` | an `engine.Step` |
| 4 | `pen` | raw pen event (only while `settings.penStream` is on) |
| 5 | `error` | `{"message": "…"}` |
| 6 | `solved` | `{"elapsedMs": 734000, "mistakes": 1, "hints": 2}` |

`state`:
```json
{
  "puzzle": {"number": 48213, "seed": 48213, "level": "hard", "hardest": "Hidden pair",
             "givens": "37.94.5.....2..7.....3...98.54.....7.........8.....93.24...9.....6..2.....8.71.25"},
  "entries": "..1..8.62589.16.43462.571..6............2...6.27.................................",
  "candidates": [0, 0, 0, "… 81 ints …"],
  "conflicts": [12, 30],
  "ink": {"2": [{"role": "answer", "points": [12.5, 30.1, 0, 1, 13.0, 34.8, 16, 1]}],
          "30": [{"role": "candidate", "points": ["…"]}]},
  "elapsedMs": 412000,
  "solved": false,
  "canUndo": true
}
```
`ink` keys are cell numbers as strings; roles are `answer | candidate`. Erased ink is simply absent.
`conflicts` lists cells whose entry repeats a digit in a row/column/box (not a check against the
solution; that's what hints are for).

`marks` (reply to `ink`, same `id`):
```json
{
  "id": 17,
  "cell": 30,
  "marks": [
    {"kind": "candidate", "digit": 8, "confidence": 0.993, "top3": [[8, 0.993], [3, 0.004], [6, 0.002]],
     "bbox": [400.0, 395.0, 22.0, 31.0], "strokes": [0]},
    {"kind": "scratch", "digit": 0, "confidence": 1, "top3": [], "bbox": [430, 400, 60, 50],
     "strokes": [1], "erases": [3]}
  ],
  "cellState": {"entry": 0, "candidates": 136, "ink": [{"role": "candidate", "points": ["…"]}]},
  "conflicts": [],
  "unknown": false,
  "canUndo": true
}
```
Kinds: `answer | candidate | scratch | unknown` (from `hwr.Mark`). `unknown: true` means at least one
mark couldn't be read confidently; the frontend should keep that ink visible and dim it so the player
rewrites it. `cellState.ink` is the cell's ink after applying the marks (scratched strokes removed).

`hint` body is exactly `engine.Step` JSON, e.g.
```json
{"technique": "Hidden single", "houses": [22], "cells": [39], "digits": [7],
 "placement": {"cell": 39, "digit": 7}, "title": "Only one place for the 7",
 "text": "In box 5, the 7 can only go in row 5, column 4. Every other spot is ruled out."}
```

`pen` body (screen pixels from the naive digitizer mapping, plus raw units for calibration):
```json
{"k": "move", "t": "pen", "sx": 512.3, "sy": 640.8, "x": 3511, "y": 4450, "p": 0.41, "ts": 1792170231016042}
```

## Frontend → backend

| Type | Name | Body |
|---|---|---|
| 100 | `hello` | `{"screen": [1404, 1872], "gridSize": 1180}` → `state` (restored game, or a new easy one) |
| 101 | `newGame` | `{"level": "hard"}` → `state` |
| 102 | `ink` | `{"id": 17, "cell": 30, "gridSize": 1180, "strokes": [[x, y, t, p, …], …]}` → `marks` |
| 103 | `setCell` | `{"cell": 30, "digit": 8}` (0 clears; tap fallback for testing) → `state` |
| 104 | `toggleCandidate` | `{"cell": 30, "digit": 8}` → `state` |
| 105 | `hint` | `{"reveal": true}` → `hint` |
| 106 | `undo` | `{}` → `state` |
| 107 | `settings` | `{"penStream": true}` → backend starts/stops sending `pen` |
| 108 | `log` | `{"message": "…"}` → backend appends to its log file |
| 112 | `view` | `{"board": false}` when the grid leaves the screen (New game list, teach page, solved page), `true` when it's back; no reply. `hello` may carry `"board"` too |

The frontend groups strokes into one `ink` message per cell once the pen has rested (~600 ms) or
starts writing in a different cell. `cell` is where the first stroke began; strokes may stick out of
it a little.

`hint`'s `reveal` says whether the player will see the whole step: `false` asks for a nudge (the
frontend shows only its `title`) and isn't counted; `true`, or no `reveal`, counts toward
`solved.hints`. The reply is the same step either way.

## Persistence (backend)

`/home/root/.local/share/ink-sudoku/state.json` (written after every change) and
`samples.jsonl` (`hwr` correction samples: when the player rewrites a misread answer in the same cell
within 20 s, log the earlier strokes with the corrected label; what counts as misread is in the
Backend notes).

## Frontend notes

Decisions the QML side (`app/`) made where the above is silent:

- **`hello` repeats** every 1 s (up to 30×) until the first `state`, since the backend may connect
  after the frontend. Answer every `hello` with the current `state`; never start a new game for it.
- **Ordering.** Before `newGame`, `hint`, `undo` or closing, the frontend sends any unsent ink first,
  and it expects replies in request order (`marks` for ink N before the `state` for a later `undo`).
  On `state`, or an `error` without `id`, it erases the live ink of `ink` messages still awaiting
  `marks` (a late `marks` still settles normally); an `error` with `id` erases just that ink. A `state` with a different `puzzle.number` or `givens` counts as a
  new game: all live ink and dimmed ink are dropped and the page flashes.
- **Unknown reads** are dimmed by the frontend alone: the strokes named by `kind: "unknown"` marks
  (indices into that `ink` message's `strokes`; if `unknown` is true but no mark has that kind, all
  of the message's strokes not in `cellState.ink`) are drawn dotted for 4 s, and go sooner when a
  stroke (or a tap) starts in that cell, the next `marks` for that cell replaces them, or a new game starts.
  They're not persisted. So **leave unknown strokes out of `cellState.ink` and `state.ink`**. Any ink
  role other than `answer`/`candidate` is drawn dotted too (and doesn't fade).
- **No ink, still shown.** An entry with no `answer` ink, or candidates in a cell with no `candidate`
  ink (`setCell`/`toggleCandidate`), are typeset in italic as a stand-in.
- **Conflicts**: every listed cell gets the marker, givens included.
- **Hints** take two presses. The first sends `hint {"reveal": false}` and shows only `title`
  (italic, larger) under the grid. The next, with the board unchanged, sets `text` under it and gives
  each of `cells` a loop (`houses`/`eliminations` aren't drawn), and sends `hint {"reveal": true}`
  so it counts. The frontend keeps the step until ink is sent or a `state` brings other
  `entries`/`candidates` or a new game, so a hint dismissed meanwhile comes back as far as it got
  without asking again. Without `title`, `text` alone is set in italic, in full at once. The hint
  clears on the next `ink` sent, any `state`, a `marks` that changes an entry, or a pen tap.
- **Taps.** A pen contact that stays within 6 px of where it landed and lifts within 180 ms (wall
  clock) is a tap, not ink: its live dot is erased and nothing is sent (in the calibration box too).
  In the grid it dismisses a visible hint, or else selects that cell (tapping it again deselects).
- **Strokes** that start outside the grid are dropped. Points are rounded to 0.1 px, `p` to 0.001.
- **`pen` stream**: `sx, sy` are taken as root-item px (root = the full 1404×1872 screen), `t` comes
  from `ts` deltas, only `t: "pen"` and `k: down|move|up` are used. The naive mapping needs
  backend-side calibration before this mode is trustworthy. While it's on, the frontend ignores the
  pen's own mouse events in the grid. `settings` is sent only when toggled (long-press the level in
  the running head); the backend should start with the stream off.
- **`log`** carries frontend-side problems (unparseable messages).

v2 (2026-10-29):

- **Ghosting.** After 12 `marks` (settled-ink updates) and whenever a hint's loops appear, the grid region (plus the 40 px ink bleed) gets a quiet refresh: a white sheet
  in a Content region covers it for 300 ms and lifts, so the region is redrawn in Content mode. It
  sits under the live ink, so writing during it still shows. Either way it waits until the pen has
  rested 1.5 s (no stroke under way or unsent). The full black flash stays on Refresh,
  new games and page changes, and resets the count. The solved page counts as the grid's refresh
  (it covers the grid in Content mode and lifts the same way).
- **Calibration** ("Teach it your hand", from the New game list) is a page of its own: running head,
  one 720 px writing box (`size: 720`), the prompted digit beside it, "N of 27" in the head. Opening
  it flashes and sends `calibrationStart`; the page writes only once `calibration` gives a `next`.
  Each sample is the box's strokes once the pen has rested 800 ms (longer than the grid's 600 ms, for
  multi-stroke digits), labelled with the `next` shown when it was sent. After a sample the box takes
  no new strokes until the following `calibration` (progress comes only from `calibration`; `calibrationRead`
  just sets the "That 5 read as 3, noted." line, or "couldn't be read" for `read: 0`) and gets a
  quiet refresh. `calibration` with `next: 0` sends `calibrationDone`; so does Finish, at any time
  (after sending any half-written sample). `personalized` shows "Your digits: read right 89% before,
  98% now." until the player taps Back to the game (no timeout). Back to the game mid-run sends
  nothing and drops unsent ink (samples already sent stay learned). An `error` while the page is up,
  without an `id` or naming a calibration sample, is shown on its read line and releases that
  sample; one naming game ink goes to the game as usual. Calibration messages arriving while
  the page is closed are ignored, and a `calibration` never opens the page by itself. No reply to
  `calibrationStart` within 3 s shows "No answer from the game yet."
- **First run.** If the first `state` of a session has `personalSamples: 0`, the tip line under the
  grid becomes a tappable nudge toward calibration until the player writes, opens it, or closes the
  app. It isn't remembered, so it comes back each session while `personalSamples` stays 0. A
  `state` without `personalSamples` (older backend) never nudges.
- **Daily.** "Today's puzzle" (first in the New game list, and on the solved page unless the solved
  game is today's daily) sends `newGame {"daily": true}`. A `puzzle.daily` date replaces "No. N" in
  the running head ("Sudoku for Thursday, 29 October", parsed as a local date). The list's note for
  it is today's level by weekday (computed by the frontend from the v2 table), "solved" if
  `stats.daily.last` is today, and "N days running" for a streak of 2+ whose `last` is today or
  yesterday.
- **Stats** in the New game list: per level "best m:ss, streak N" (each part only when non-zero;
  "not solved yet" / "not played yet" otherwise). `solved` patches the frontend's copy (the
  streak, and `bestMs = elapsedMs` when `best`), since a solve completed by ink brings no `state`.
- **Solved page.** `solved` opens a page over the grid and the foot (the running head stays):
  "Solved", the time in words, "A new best for hard puzzles." when `best`, the streak when 2+
  ("Five days running." for a daily, "Three hard puzzles in a row." otherwise), mistakes and hints,
  and New game (opens the list over the page) / Today's puzzle / See the grid. It stays until one
  of those or a new game. A restored game that is already solved shows only the line under the grid.
  That line follows `solved` (a solve by ink brings no `state`); a `marks` that changes an entry of a
  solved board clears it.

## Backend notes

Decisions the Go side (`backend/cmd/sudoku`, `backend/game`) made where the above is silent:

- **Each `ink` message is read on its own.** Stored ink only matters to scratch-outs, which can
  erase it. Writing over an old answer replaces it (no merging with the old strokes). A digit split
  across two messages (pen rested mid-digit) is read as two parts. Per-stroke `t` is fine: the
  strokes of one message count as written in one go.
- **`marks` indices**: `strokes` and `erases` both index that message's `strokes`. Each non-empty
  message stroke appears once, either in a mark's `strokes` or in a scratch's `erases` (rubbed out
  in the same message). Stored ink a scratch erased isn't listed; it's just gone from
  `cellState.ink`. Strokes `hwr` skipped (mostly outside the cell) come back as one extra `unknown`
  mark. `erases` and `top3` are always present (`[]` when empty); `bbox` is `[x, y, w, h]` in grid px.
- **Ink items carry `"digit"`** (the digit the stroke stands for). Stored points are rounded to
  0.1 px, 1 ms and 0.01 pressure.
- **Scratch-outs erase whole digits**: touching any answer stroke clears the entry and all answer
  ink; touching a candidate's stroke clears that candidate and all its ink.
- **Writing a candidate that is already set turns it off** and removes that digit's ink (the new
  strokes aren't stored), so candidate ink always matches the bits. `toggleCandidate` off removes
  the digit's ink too; `setCell` to a different digit drops the answer ink.
- **Given cells**: `ink` there is ignored (`marks: []`, `unknown: false`, empty `cellState`);
  `setCell`/`toggleCandidate` there reply with the unchanged `state`.
- **`conflicts`** lists entered cells only (never givens) whose digit repeats in a peer, given or
  entered.
- **`puzzle.number` = `seed`**, drawn from 1–999999. `newGame` also accepts an optional `"seed"`.
- **`solved`** follows the `state`/`marks` reply of the change that completes the board.
  `mistakes` counts entries set to a digit that isn't the solution's; `hints` counts `hint`s
  answered with `reveal` true or absent (full hints shown); `hint` on a solved board replies
  `error`. The timer counts wall time from `hello` until AppLoad reports the frontend gone or the
  backend exits, thinking time included, and pauses while solved, while the frontend reports the
  grid off screen (`view`), and while the display sleeps (xochitl's log: "Changing display state
  from Normal to DeepSleep|LightSleep", back "to Normal"; `display.Watch`). Wall time, because the
  kernel suspends between strokes with the page on screen and the monotonic clock stops then. A
  frontend message while asleep counts as a wake. One stretch with no event (frontend message,
  `view`, display change) adds at most 30 min (`game.MaxGap`), in case a sleep goes unnoticed.
- **Correction samples.** An answer rewritten in the same cell within 20 s as a different digit is
  logged as a misread of the earlier strokes only if the earlier read was unsure (confidence below
  0.995) or the new answer is written over the old one: at least half of its ink box inside the old
  ink's, both boxes grown by 3% of a cell so a straight 1 has an area. Otherwise it's a change of
  mind, and so is any new digit after a scratch-out erased the old answer or an `undo` or `setCell`
  took it away.
- **Undo**: one step per message that changed something, 200 deep, saved in `state.json`;
  `newGame` clears it.
- **`gridSize`**: stored ink is kept in the latest `hello`/`ink` grid px and rescaled if it changes.
- **Files** in `$INK_SUDOKU_DIR` (default `/home/root/.local/share/ink-sudoku/`): `state.json`,
  `samples.jsonl`, `backend.log` (rotated at 1 MB). An unreadable `state.json` is moved to
  `state.json.bad` and `hello` starts a new easy game. A sample-log line that doesn't parse (torn by a
  power loss mid-append) is skipped at startup, and the next append starts a fresh line.
- **`pen`**: `sx = (x − min)/(max − min) · screen width` from the evdev ranges (same for `sy`), no
  rotation or calibration yet; `p` 0–1; `ts` µs. The stream starts off; if the digitizer can't be
  opened, `settings` replies `error`.

v2 (where the v2 section below is silent):

- **Daily seed** is the date as `YYYYMMDD` (`2026-10-29` → `20261029`), so a daily's `number`/`seed`
  is outside the random 1–999999 range. `engine.Generate` is deterministic per seed and level, so no
  retry layer is needed. It runs synchronously on `newGame` (tens of ms on a Mac, likely under ~1 s on
  the tablet for expert). The date and weekday come from the backend's clock in the tablet's
  local timezone.
- **`newGame {"daily": true}` while today's daily is the current game** just resends `state`
  (progress kept, not counted again). Yesterday's unfinished daily is replaced like any game.
- **Stats.** `played` counts every game the backend starts (`hello`'s first game, `newGame`, a
  daily), never a restored one. A daily also counts toward its level. Starting a game while the
  current one was never solved resets *that* game's level streak. A game counts as solved once;
  undoing and re-solving sends `solved` again but changes no stats (`best: false`). The first solve
  at a level is `best: true`. `solved.streak` is the daily streak for a daily puzzle, else the
  level's. A daily counts for its own date even if finished after midnight. In `state.stats` a daily
  streak whose `last` is older than yesterday reads 0. `bestMs` is 0 before the first solve. All
  four levels and `daily` are always present. The `state` sent with the completing change already
  includes the new stats; `marks` doesn't carry stats.
- **Calibration.** After every `calibrationRead` the backend also sends `calibration` with the new
  `done`/`next`, so the frontend never has to compute the order. The order is 1–9, then two fixed
  shuffles (`3 7 1 9 5 2 8 4 6`, `8 2 6 4 9 1 5 3 7`). The label is the `digit` in the message, and
  every sample is kept, including ones the recognizer couldn't read. `calibrationInk` without
  `calibrationStart` continues the current run; samples after the 27th are still learned, and `done`
  stays 27, `next` 0. A bad `digit`/`size`, a malformed stroke or no ink replies `error` with the
  `id`. `read` is 0 when the recognizer rejects the glyph. The whole box's ink is read as one glyph
  (no answer/candidate split). Calibration ink never reaches the game (board, undo, ink). The game
  timer pauses during calibration (the frontend sends `view` `{"board": false}`). Progress is in memory only, while samples persist.
  `samples`, `personalSamples` and `personalized.samples` are `hwr.Personal.Len()` (samples it
  could use). `before`/`after` are rounded to 0.001.
- **Files** add `stats.json` (written atomically; an unreadable one is moved to `stats.json.bad`)
  and `calibration.jsonl` (`cell` is the box `[0, 0, size, size]` in box px).
- **Startup.** `hwr.NewPersonal` (one CNN pass per sample, ~0.5 s for 200 on the tablet) runs in a
  goroutine, so `hello` is answered at once. Until it's swapped in, the plain model reads ink and
  calibration samples, and `personalSamples`/`samples` report what it will hold; samples learned
  meanwhile are added to it when it's swapped in.

## v2 additions (2026-10-29)

New messages; everything above still holds.

| Type | Dir | Name | Body |
|---|---|---|---|
| 7 | b→f | `calibration` | `{"total": 27, "done": 4, "next": 5, "samples": 31}` — progress of "teach it your hand"; `next` is the digit to write next (1–9, three rounds), 0 when finished |
| 8 | b→f | `calibrationRead` | `{"id": 9, "digit": 5, "read": 5, "confidence": 0.91, "done": 5, "total": 27}` — reply to one calibration sample: what the *current* recognizer read |
| 9 | b→f | `personalized` | `{"samples": 58, "before": 0.89, "after": 0.98}` — sent after `calibrationDone`; leave-one-out accuracy on the player's own samples without / with personalization |
| 109 | f→b | `calibrationStart` | `{}` → `calibration` (restarts the round count; keeps earlier samples) |
| 110 | f→b | `calibrationInk` | `{"id": 9, "digit": 5, "size": 260, "strokes": [[x, y, t, p, …]]}` → `calibrationRead`. Strokes in box-local px; `size` is the square writing box's side |
| 111 | f→b | `calibrationDone` | `{}` → `personalized`, then the normal `state` |

Changes to existing messages:
- `newGame` accepts `{"daily": true}`: today's puzzle (seed from the local date, level by weekday:
  Mon/Tue easy, Wed/Thu medium, Fri/Sat hard, Sun expert). `state.puzzle` gains `"daily": "2026-10-29"`
  (empty string otherwise).
- `state` gains `"stats"`: `{"easy": {"played": 3, "solved": 2, "bestMs": 312000, "streak": 2}, …,
  "daily": {"streak": 4, "last": "2026-10-29"}}` and `"personalSamples": 58`.
- `solved` gains `"best": true` (new best time for the level) and `"streak": 3`.
- `hint` (an `engine.Step`) gains `"title"`: the idea in plain words ("Only one place for the 7"),
  and `text` becomes a plain teaching sentence using "row 4, column 4" / "box 5" wording instead of
  "r4c4". Technique names stay in `technique`.

Persistence adds `calibration.jsonl` (labelled samples, `hwr.Sample` with `source: "calibration"`) next
to `samples.jsonl` (corrections). Both feed `hwr.NewPersonal` at startup; new samples are `Add`ed live.
