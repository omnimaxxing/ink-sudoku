import QtQuick
import QtQuick.Window
import "stubs/net/asivery/AppLoad/inksudoku.js" as InkSudoku
import "fixtures/session.js" as Session

// Loads an app's root QML at Paper Pure size, saves a screenshot, quits.
// Fonts the tablet has but the Mac lacks go in dev/fonts/ (optional, not shipped).
//
// Ink Sudoku flags (before the app path; stub-side flags are in inksudoku.js):
//   --chooser            open the New game list
//   --number             …then write 4 8 2 1 3 7 in its Puzzle No. boxes (the stub reads them so)
//   --number-play LEVEL  …then tap LEVEL to play that number
//   --ink                write a 7 into r5c4 (the stub reads it as unknown: dotted)
//   --ink-again          …then start another stroke there (the dotted read goes at once)
//   --ink-many N         write N digits into empty cells, one per step
//   --hint-press N       press Hint N times (1: the title alone; 2: text and loops too)
//   --tap                a quick pen tap in an empty cell (selects it, or dismisses a hint)
//   --pencil-one         a quick 24 px pencil 1 in another empty cell (ink, not a tap)
//   --trace-refresh      log when the grid's quiet refresh runs
//   --teach              open "Teach it your hand"
//   --teach-write N      …and write the first N prompted digits there
//   --teach-live         …then start the next digit (live ink in the box)
//   --teach-finish       …then finish (the before/after page)
//   --see-grid           close the solved page ("See the grid")
//   --new-game LEVEL     start a new game at LEVEL (easy|medium|hard|expert) first
//   --grab-at MS         take the screenshot at MS ms (default 1500)
//   --replay [N]         the stub answers with the real backend's recorded messages
//                        (dev/fixtures/session.json) and this script redoes the session's
//                        steps 2..N (default all) through the app: recorded pen strokes
//                        into the writers, Hint/Undo/New game, the calibration page. The
//                        stub logs "replay: …" wherever the app's request differs.
//                        Regenerate the fixtures: see backend/cmd/sudoku/fixtures_test.go
//                        (keep dev/fixtures/session.js: canned mode imports it too).
//   --parts FILE         the stub answers hello with a real backend's wire messages for a
//                        restored game, big ones in parts (backend/cmd/sudoku/restore_test.go);
//                        run with QML_XHR_ALLOW_FILE_READ=1
//
// Ink Kakuro (kakuro-app/ui/main.qml): --chooser, --hint-press, --see-grid as above; --ink,
// --ink-again, --ink-many, --tap and --pencil-one use the puzzle's own n×n grid and its empty
// white cells (--ink: the first one). Kakuro only:
//   --ink-edge           a 1 whose stroke starts just inside the clue cell left of an
//                        empty white cell (it belongs to the white cell: see the `ink` cell)
//   --ink-clue           a stroke deep inside a clue cell (dropped: nothing is sent)
// Stub-side flags (--level, --hint, --hint-lone, --daily, --solved, --first-run, --blank)
// are in inkkakuro.js.
Window {
    id: w
    width: 1404
    height: 1872
    visible: true
    readonly property var args: Qt.application.arguments
    property string app: args[args.length - 2]
    property string out: args[args.length - 1]
    function has(flag) { return args.indexOf(flag) >= 0; }
    function num(flag, dflt) {
        const i = args.indexOf(flag);
        return i >= 0 ? Number(args[i + 1]) : dflt;
    }

    FontLoader { id: garamond; source: "fonts/EBGaramond-VF.ttf" }
    FontLoader { id: garamondItalic; source: "fonts/EBGaramond-Italic-VF.ttf" }
    Loader {
        id: loader
        anchors.fill: parent
        active: garamond.status !== FontLoader.Loading && garamondItalic.status !== FontLoader.Loading
        source: w.app
        onStatusChanged: if (status === Loader.Error) { console.log("LOAD ERROR"); Qt.exit(1); }
    }

    function find(item, name) {
        if (!item)
            return null;
        if (item.objectName === name)
            return item;
        for (const c of item.children) {
            const f = find(c, name);
            if (f)
                return f;
        }
        return null;
    }
    // Grid geometry of the loaded app: an Ink Kakuro puzzle says its size, Sudoku is 9×9.
    function kakuro(app) { return !!(app.puzzle && app.puzzle.cells); }
    function gridN(app) { return kakuro(app) && app.puzzle.n > 0 ? app.puzzle.n : 9; }
    // Empty cells to write in (fixed at first use): Sudoku's from the canned givens and
    // entries, Kakuro's the white cells the app shows without an entry or ink.
    property var empties: null
    function emptyCells(app) {
        if (empties)
            return empties;
        const out = [];
        if (kakuro(app)) {
            for (let c = 0; c < app.puzzle.cells.length; c++)
                if (!app.puzzle.cells[c].b && !app.entries[c] && !app.inkMap[String(c)])
                    out.push(c);
        } else {
            for (let c = 0; c < 81; c++)
                if (InkSudoku.GIVENS[c] === "." && InkSudoku.ENTRIES[c] === ".")
                    out.push(c);
        }
        empties = out;
        return out;
    }
    function penTo(wr) {
        return (k, x, y, p, ms) => k === "down" ? wr.down(x, y, p, ms) : k === "move" ? wr.move(x, y, p, ms) : wr.up();
    }

    // Feeds one synthesized digit, as pen samples, to `pen(k, x, y, p, ms)`.
    function scribble(pen, d, cx, cy, h) {
        const t0 = Date.now();
        for (const s of InkSudoku.write(d, cx, cy, h)) {
            for (let i = 0; i < s.length; i += 4)
                pen(i === 0 ? "down" : "move", s[i], s[i + 1], 1, t0 + s[i + 2]);
            pen("up");
        }
    }

    // The script: one step every 120 ms once the app has loaded.
    property var steps: []
    Timer {
        id: script
        interval: 120
        repeat: true
        running: loader.status === Loader.Ready
        property bool built: false
        onTriggered: {
            if (!built) {
                built = true;
                w.steps = w.buildSteps();
            }
            const f = w.steps.shift();
            if (f)
                f();
            else
                stop();
        }
    }
    // Recorded strokes [x, y, t, p, …] as pen samples.
    function feed(pen, strokes) {
        const t0 = Date.now();
        for (const st of strokes) {
            for (let i = 0; i < st.length; i += 4)
                pen(i === 0 ? "down" : "move", st[i], st[i + 1], st[i + 3], t0 + st[i + 2]);
            pen("up");
        }
    }
    // Does what the player did to make the app send step `st`'s request.
    function replayStep(app, st) {
        const b = st.send.body, t = st.send.type, cal = find(app, "calibration");
        console.log("replay: step", st.step, st.note);
        if (t < 109 && app.page === "teach")
            cal.leave();
        if (app.solvedOpen && (t === 102 || t === 105 || t === 106))
            app.solvedOpen = false;     // "See the grid"
        if (t === 101 && b.daily)
            app.newDaily();
        else if (t === 101 && ["easy", "medium", "hard", "expert"].indexOf(b.level) >= 0)
            app.newGame(b.level);
        else if (t === 102) {
            const wr = find(app, "gameWriter");
            feed((k, x, y, p, ms) => k === "down" ? wr.down(x, y, p, ms) : k === "move" ? wr.move(x, y, p, ms) : wr.up(), b.strokes);
            wr.flush();
        } else if (t === 105)
            app.askHint();
        else if (t === 106)
            app.undo();
        else if (t === 109)
            app.openCalibration();
        else if (t === 110) {
            feed(cal.pen, b.strokes);
            cal.writer.flush();
        } else if (t === 111)
            cal.finish();
        else
            app.send(t, b);     // not something the app sends by itself
    }
    function buildSteps() {
        const app = loader.item, s = [];
        s.push(() => {});   // let `state` arrive
        if (has("--replay")) {
            const n = num("--replay", 0);
            for (const st of Session.steps.slice(1, n > 0 ? n : Session.steps.length))
                s.push(() => replayStep(app, st));
        }
        if (has("--new-game"))
            s.push(() => app.newGame(args[args.indexOf("--new-game") + 1]));
        if (has("--see-grid"))
            s.push(() => { app.solvedOpen = false; });
        if (has("--chooser"))
            s.push(() => { app.chooserOpen = true; });
        if (has("--number"))
            for (let i = 0; i < 6; i++)
                s.push(() => {
                    const wr = find(app, "numberWriter"), B = app.numberBox;
                    scribble(penTo(wr), Number("482137"[i]), (i + 0.5) * B, 0.5 * B, B * 0.6);
                    wr.flush();
                });
        if (has("--number-play"))
            s.push(() => app.newGame(args[args.indexOf("--number-play") + 1]));
        // Sudoku writes in r5c4; Kakuro in its first empty white cell.
        const inkCell = () => kakuro(app) ? emptyCells(app)[0] : 39;
        if (has("--ink"))
            s.push(() => {
                const wr = find(app, "gameWriter"), N = gridN(app), S = 1180 / N, c = inkCell();
                scribble(penTo(wr), 7, (c % N + 0.5) * S, (Math.floor(c / N) + 0.5) * S, S * 0.55);
                wr.flush();
            });
        if (has("--ink-again"))
            s.push(() => {
                const wr = find(app, "gameWriter"), N = gridN(app), S = 1180 / N, c = inkCell();
                wr.down((c % N + 0.3) * S, (Math.floor(c / N) + 0.3) * S, 1);
                wr.move((c % N + 0.6) * S, (Math.floor(c / N) + 0.7) * S, 1);
            });
        for (let i = 0; i < num("--ink-many", 0); i++)
            s.push(() => {
                const wr = find(app, "gameWriter"), N = gridN(app), S = 1180 / N, empty = emptyCells(app);
                const c = empty[i % empty.length];
                scribble(penTo(wr), 1 + i % 9, (c % N + 0.5) * S, (Math.floor(c / N) + 0.5) * S, S * 0.55);
                wr.flush();
            });
        if (has("--ink-edge"))
            s.push(() => {
                const wr = find(app, "gameWriter"), N = gridN(app), S = 1180 / N;
                const c = emptyCells(app).find(k => app.puzzle.cells[k - 1] && app.puzzle.cells[k - 1].b);
                // a 1 hugging the left line: its flag starts 8 px inside the clue cell
                const pts = InkSudoku.write(1, (c % N + 0.5) * S, (Math.floor(c / N) + 0.5) * S, S * 0.55);
                const dx = (c % N) * S - 8 - pts[0][0];
                pts[0] = pts[0].map((v, i) => i % 4 === 0 ? v + dx : v);
                console.log("ink-edge: cell", c, "first stroke starts at x", (pts[0][0] - (c % N) * S).toFixed(1), "px from its left edge");
                feed(penTo(wr), pts);
                wr.flush();
            });
        if (has("--ink-clue"))
            s.push(() => {
                const wr = find(app, "gameWriter"), N = gridN(app), S = 1180 / N;
                const c = app.puzzle.cells.findIndex((k, i) => k.b && i % N > 0 && Math.floor(i / N) > 0);
                const x = (c % N + 0.5) * S, y = (Math.floor(c / N) + 0.5) * S;
                console.log("ink-clue: clue cell", c, "down() took it:", wr.down(x, y, 1));
                wr.move(x + 20, y + 30, 1);
                wr.up();
                wr.flush();
            });
        for (let i = 0; i < num("--hint-press", 0); i++)
            s.push(() => app.askHint());
        if (has("--tap"))
            s.push(() => {
                const wr = find(app, "gameWriter"), N = gridN(app), S = 1180 / N, empty = emptyCells(app);
                const c = empty[20 % empty.length];
                const x = (c % N + 0.5) * S, y = (Math.floor(c / N) + 0.5) * S;
                wr.down(x, y, 1);
                wr.move(x + 2, y + 1, 1);
                wr.up();
            });
        if (has("--pencil-one"))
            s.push(() => {
                const wr = find(app, "gameWriter"), N = gridN(app), S = 1180 / N, empty = emptyCells(app);
                const c = empty[21 % empty.length];
                const x = (c % N + 0.2) * S, y = (Math.floor(c / N) + 0.1) * S;
                wr.down(x, y, 1);
                for (let k = 1; k <= 6; k++)
                    wr.move(x - 0.3 * k, y + 4 * k, 1);
                wr.up();
                wr.flush();
            });
        if (has("--teach")) {
            s.push(() => app.openCalibration());
            s.push(() => {});
            const cal = () => find(app, "calibration");
            for (let i = 0; i < num("--teach-write", 0); i++)
                s.push(() => {
                    const c = cal();
                    scribble(c.pen, c.next, c.boxSize * 0.48, c.boxSize * 0.5, c.boxSize * 0.62);
                    c.writer.flush();
                });
            if (has("--teach-live"))
                s.push(() => { const c = cal(); scribble(c.pen, c.next, c.boxSize * 0.5, c.boxSize * 0.5, c.boxSize * 0.6); });
            if (has("--teach-finish"))
                s.push(() => cal().finish());
        }
        return s;
    }

    property real t0: Date.now()
    Timer {
        interval: 50
        repeat: true
        running: w.has("--trace-refresh") && loader.status === Loader.Ready
        property bool was: false
        onTriggered: {
            const q = w.find(loader.item, "gridQuiet");
            if (q && q.running !== was) {
                was = q.running;
                console.log("quiet refresh", was ? "on" : "off", "at", Date.now() - w.t0, "ms");
            }
        }
    }
    Timer {
        interval: w.num("--grab-at", w.has("--replay") ? 120 * Session.steps.length + 1200 : 1500)
        running: true
        onTriggered: loader.item.grabToImage(r => { r.saveToFile(w.out); console.log("saved", w.out); Qt.quit(); })
    }
}
