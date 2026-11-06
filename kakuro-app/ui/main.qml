pragma ComponentBehavior: Bound
import QtQuick
import net.asivery.AppLoad 1.0
import net.asivery.ApploadUtils 1.0

// Ink Kakuro: a pen-only Kakuro page. The Go backend owns the game (KAKURO.md, on top
// of PROTOCOL.md); this side renders it and captures ink.
Rectangle {
    id: root
    anchors.fill: parent
    color: "white"

    signal close
    function unloading() {
        writer.flush();
        endpoint.terminate();
    }

    // Message types (PROTOCOL.md; KAKURO.md has no calibration messages).
    readonly property int msgState: 1
    readonly property int msgMarks: 2
    readonly property int msgHint: 3
    readonly property int msgPen: 4
    readonly property int msgError: 5
    readonly property int msgSolved: 6
    readonly property int msgPart: 50
    readonly property int msgHello: 100
    readonly property int msgNewGame: 101
    readonly property int msgInk: 102
    readonly property int msgHintRequest: 105
    readonly property int msgUndo: 106
    readonly property int msgSettings: 107
    readonly property int msgLog: 108

    readonly property real gridSize: 1180
    readonly property real margin: Math.round((width - gridSize) / 2)
    readonly property real gridTop: 170

    // Game, as last told by the backend.
    property var puzzle: null
    // Cells per side: 9 until the first puzzle says otherwise.
    readonly property int n: !puzzle ? 9 : puzzle.n > 0 ? puzzle.n
        : puzzle.cells && puzzle.cells.length > 0 ? Math.round(Math.sqrt(puzzle.cells.length)) : 9
    readonly property int cellCount: n * n
    property var openCells: []      // per cell: true for white cells (they take ink)
    property var entries: []        // n*n digits, 0 = empty
    property var candidates: []
    property var conflicts: []
    property bool canUndo: false
    property bool solved: false
    property var inkMap: ({})       // cell (string) -> [{role, points}]
    property var dimInk: ({})       // cell -> [points]: unknown reads, until they fade
    property var dimUntil: ({})     // cell -> ms (Date.now) when its dimmed ink fades
    property int dimCount: 0
    readonly property int dimMs: 4000
    property var inkRoles: []       // cell -> 1 answer ink | 2 candidate ink
    property var hint: null         // kakuro.Step on screen, or null
    // Hints come in two steps: the title alone (a nudge), then the text and cell loops.
    property var hintStep: null     // the step for the board as it is; outlives a dismissed hint
    property bool hintRevealed: false // hintStep has been shown in full (the backend counted it)
    property bool hintReveal: true  // what the last `hint` request asked for
    property string note: ""        // solved / error / waiting, shown where hints go
    property string solvedNote: ""  // from `solved`, kept while the game stays solved
    property var solvedInfo: ({})   // the last `solved` body
    property bool solvedOpen: false // the solved page is up
    property var stats: ({})        // state.stats
    property int personalSamples: -1
    property bool firstRun: false   // no handwriting samples yet: point at Ink Sudoku
    property bool firstRunSeen: false

    // Ghosting: settled-ink updates since the last clean refresh of the grid.
    property int settledUpdates: 0
    readonly property int settledUpdatesMax: 12
    property bool refreshDue: false
    property real lastPenAt: 0

    // Input modes. Long-press the left running head: finger writes (testing without
    // a pen). Long-press the right one: pen samples come from the backend stream.
    property bool fingerWrites: false
    property bool penStream: false
    property int selected: -1
    property bool chooserOpen: false

    AppLoad {
        id: endpoint
        applicationID: "ink-kakuro"
        onMessageReceived: (type, contents) => root.onBackend(type, contents)
    }

    function send(type, body) {
        endpoint.sendMessage(type, JSON.stringify(body));
    }

    // The backend may start after us, or still be making the first puzzle (it then sends
    // `state` unasked): say hello until a state arrives.
    property int hellos: 0
    function hello() {
        hellos++;
        send(msgHello, { screen: [Math.round(width) || 1404, Math.round(height) || 1872], gridSize: gridSize });
    }
    Component.onCompleted: hello()
    Timer {
        id: helloTimer
        interval: 1000
        repeat: true
        running: root.puzzle === null && root.hellos < 30
        onTriggered: root.hello()
    }

    // A message too big for one packet comes in msgPart pieces, each
    // "<type> <index> <count>\n<slice>"; the slices joined are the message.
    property var parts: null
    function joinPart(contents) {
        const nl = contents.indexOf("\n");
        const head = contents.substring(0, nl).split(" ").map(Number);
        if (head[1] === 0)
            parts = { type: head[0], count: head[2], slices: [] };
        if (!parts || parts.type !== head[0] || parts.slices.length !== head[1]) {
            parts = null;
            send(msgLog, { message: "frontend: dropped a message part out of step: " + contents.substring(0, nl) });
            return;
        }
        parts.slices.push(contents.substring(nl + 1));
        if (parts.slices.length < parts.count)
            return;
        const whole = parts;
        parts = null;
        onBackend(whole.type, whole.slices.join(""));
    }

    function onBackend(type, contents) {
        if (type === msgPart) {
            joinPart(contents);
            return;
        }
        let m;
        try {
            m = contents ? JSON.parse(contents) : {};
        } catch (e) {
            console.log("ink-kakuro: bad message", type, e);
            send(msgLog, { message: "frontend: unparseable message of type " + type + ": " + e });
            return;
        }
        if (type === msgState)
            applyState(m);
        else if (type === msgMarks)
            applyMarks(m);
        else if (type === msgHint) {
            const looped = hint !== null && hintRevealed;
            hintStep = m;
            hintRevealed = hintReveal || !m.title;
            hint = m;
            note = "";
            if (hintRevealed && !looped)
                quietRefresh();
        } else if (type === msgPen)
            onPen(m);
        else if (type === msgError) {
            if (m.id)
                writer.settle(m.id);    // that ink wasn't taken: its live strokes go
            else
                writer.dropPending();
            hint = null;
            note = m.message || "Something went wrong.";
        } else if (type === msgSolved) {
            solved = true;      // a solve by ink brings no `state`
            solvedNote = solvedText(m);
            note = solvedNote;
            solvedInfo = m;
            noteSolve(m);
            chooserOpen = false;
            solvedOpen = true;
            // The page covers the grid in Content mode, and lifts the same way: that's
            // the grid's clean refresh.
            settledUpdates = 0;
            refreshDue = false;
        }
    }

    // n*n ints from the backend, or zeros if missing or the wrong length.
    function cellInts(a, count) {
        const out = [];
        for (let c = 0; c < count; c++)
            out.push(a && a.length === count ? Number(a[c]) || 0 : 0);
        return out;
    }
    // A seed can come up again at another level, so a new game is a new number or level.
    function samePuzzle(a, b) {
        return a.number === b.number && a.level === b.level;
    }

    function applyState(s) {
        const p = s.puzzle || {};
        const fresh = !puzzle || !samePuzzle(p, puzzle);
        const first = !puzzle;
        if (fresh) {
            writer.reset();
            dimInk = {};
            dimUntil = {};
            dimCount = 0;
            selected = -1;
            solvedNote = "";
            solvedOpen = false;
        } else {
            writer.dropPending();
        }
        if (s.stats)
            stats = s.stats;
        if (s.personalSamples !== undefined) {
            personalSamples = s.personalSamples;
            if (personalSamples === 0 && !firstRunSeen)
                firstRun = true;
            firstRunSeen = true;
        }
        puzzle = p;     // sets n, and with it the board's, writer's and settled ink's grids
        const count = cellCount;
        const cells = p.cells || [];
        const open = [];
        for (let c = 0; c < count; c++)
            open.push(!!cells[c] && !cells[c].b);
        openCells = open;
        const ents = cellInts(s.entries, count), cands = cellInts(s.candidates, count);
        if (fresh || String(ents) !== String(entries) || String(cands) !== String(candidates))
            forgetHint();
        entries = ents;
        candidates = cands;
        conflicts = s.conflicts || [];
        canUndo = !!s.canUndo;
        solved = !!s.solved;
        inkMap = s.ink || {};
        const roles = [];
        for (let c = 0; c < count; c++)
            roles.push(rolesOf(inkMap[String(c)]));
        inkRoles = roles;
        settled.setAll(inkMap, dimInk);
        hint = null;
        note = solved ? solvedNote || "Solved." : "";
        if (fresh && !first)
            flash.start();
    }

    function applyMarks(m) {
        const sent = writer.settle(m.id);
        if (m.canUndo !== undefined)
            canUndo = !!m.canUndo;
        const c = m.cell;
        if (c === undefined || c < 0 || c >= cellCount || entries.length !== cellCount)
            return;
        const cs = m.cellState || {};
        const ink = cs.ink || [];
        const before = entries[c];
        if (cs.entry !== undefined) {
            const ents = entries.slice();
            ents[c] = Number(cs.entry) || 0;
            entries = ents;
        }
        if (cs.candidates !== undefined) {
            const cands = candidates.slice();
            cands[c] = cs.candidates;
            candidates = cands;
        }
        if (m.conflicts)
            conflicts = m.conflicts;

        // Unknown reads stay on the page, dotted, for dimMs or until the cell is written again.
        const dim = [];
        if (sent) {
            const marks = m.marks || [];
            for (const mk of marks)
                if (mk.kind === "unknown")
                    for (const i of mk.strokes || [])
                        if (sent.strokes[i])
                            dim.push(sent.strokes[i]);
            if (m.unknown && dim.length === 0)
                for (const s of sent.strokes)
                    dim.push(s);
        }
        const kept = dim.filter(pts => !ink.some(s => samePoints(s.points, pts)));
        const d = Object.assign({}, dimInk);
        d[c] = kept;
        dimInk = d;
        if (kept.length > 0)
            dimUntil[c] = Date.now() + dimMs;
        else
            delete dimUntil[c];
        dimCount = Object.keys(dimUntil).length;

        const im = Object.assign({}, inkMap);
        im[String(c)] = ink;
        inkMap = im;
        const roles = inkRoles.slice();
        roles[c] = rolesOf(ink);
        inkRoles = roles;
        settled.setCell(c, ink, kept);
        if (entries[c] !== before) {
            hint = null;
            if (solved) {       // a changed entry unsolves; `solved` follows if it solves again
                solved = false;
                note = "";
            }
        }
        if (++settledUpdates >= settledUpdatesMax)
            refreshDue = true;
    }

    // Dotted unknown reads go after dimMs, or as soon as a stroke starts in their cell.
    function undim(c) {
        if (!dimInk[c] || dimInk[c].length === 0)
            return;
        const d = Object.assign({}, dimInk);
        delete d[c];
        dimInk = d;
        delete dimUntil[c];
        dimCount = Object.keys(dimUntil).length;
        settled.setCell(c, inkMap[String(c)], []);
    }
    Timer {
        interval: 250
        repeat: true
        running: root.dimCount > 0
        onTriggered: {
            const now = Date.now();
            for (const c in root.dimUntil)
                if (root.dimUntil[c] <= now)
                    root.undim(Number(c));
        }
    }

    // Ghosting policy: fast-mode ink leaves ghosts, so after settledUpdatesMax settled
    // updates (once the pen has rested), or when a hint appears, the grid region gets a
    // quiet Content-mode refresh. The full black flash is on holding Refresh and on new games.
    function penIdle() {
        return !writer.writing && writer.group === null && Date.now() - lastPenAt > 1500;
    }
    function quietRefresh() {
        if (!penIdle()) {
            refreshDue = true;
            return;
        }
        refreshDue = false;
        settledUpdates = 0;
        gridQuiet.run();
    }
    // Refresh tapped: the player asked, so don't wait for the pen to rest.
    function refreshNow() {
        refreshDue = false;
        settledUpdates = 0;
        gridQuiet.run();
    }
    Timer {
        interval: 500
        repeat: true
        running: root.refreshDue && !root.solvedOpen
        onTriggered: if (root.penIdle()) root.quietRefresh()
    }

    // A solve completed by ink brings `marks` + `solved` but no `state`: carry its news
    // into the chooser's stats until the next `state` brings the backend's own.
    function noteSolve(m) {
        if (!puzzle || m.streak === undefined)
            return;
        const st = Object.assign({}, stats);
        if (puzzle.daily) {
            st.daily = Object.assign({}, st.daily, { streak: m.streak, last: puzzle.daily });
        } else {
            st[puzzle.level] = Object.assign({}, st[puzzle.level], { streak: m.streak });
        }
        if (m.best && m.elapsedMs)
            st[puzzle.level] = Object.assign({}, st[puzzle.level], { bestMs: m.elapsedMs });
        stats = st;
    }

    function rolesOf(strokes) {
        let r = 0;
        for (const s of strokes || [])
            r |= s.role === "answer" ? 1 : s.role === "candidate" ? 2 : 0;
        return r;
    }
    function samePoints(a, b) {
        if (!a || !b || a.length !== b.length)
            return false;
        for (let i = 0; i < a.length; i += 4)
            if (Math.abs(a[i] - b[i]) > 0.5 || Math.abs(a[i + 1] - b[i + 1]) > 0.5)
                return false;
        return true;
    }
    function isOpen(c) {
        return c >= 0 && !!openCells[c];
    }

    // Backend pen stream (only while penStream is on): screen px, taken as root px.
    function onPen(e) {
        if (!penStream || !puzzle || (e.t && e.t !== "pen"))
            return;
        const ms = e.ts ? e.ts / 1000 : undefined;
        const p = e.p === undefined ? 1 : e.p;
        const q = grid.mapFromItem(root, e.sx, e.sy);
        if (e.k === "down")
            writer.down(q.x, q.y, p, ms);
        else if (e.k === "move")
            writer.move(q.x, q.y, p, ms);
        else if (e.k === "up")
            writer.up();
    }

    // Does this mouse event write? mouse.source 0 is the pen; anything else is a
    // finger synthesized by the system.
    function writes(mouse) {
        if (mouse.source === 0)
            return !penStream;
        return fingerWrites;
    }

    function act(f) {
        writer.flush();
        chooserOpen = false;
        f();
    }
    function newGame(level) {
        act(() => send(msgNewGame, { level: level }));
    }
    function newDaily() {
        act(() => send(msgNewGame, { daily: true }));
    }
    // First press: the step's title alone, asked for with `reveal: false` (not counted).
    // The next press on the same board shows its text and cell loops and sends
    // `reveal: true`, which counts it. The step outlives a dismissed hint, so pressing
    // again brings it back as far as it got without asking the backend.
    function askHint() {
        act(() => {
            note = "";
            if (!hintStep) {
                hintReveal = false;
                send(msgHintRequest, { reveal: false });
                return;
            }
            const looped = hint !== null && hintRevealed;
            if (!hintRevealed) {
                hintRevealed = true;
                hintReveal = true;
                send(msgHintRequest, { reveal: true });
            }
            hint = hintStep;
            if (!looped)
                quietRefresh();
        });
    }
    // The board may have changed (ink sent, a different `state`): hints start over.
    function forgetHint() {
        hint = null;
        hintStep = null;
        hintRevealed = false;
    }
    function undo() {
        act(() => send(msgUndo, {}));
    }
    function setPenStream(on) {
        writer.flush();
        penStream = on;
        send(msgSettings, { penStream: on });
    }

    function grouped(n) {
        return String(n).replace(/\B(?=(\d{3})+(?!\d))/g, ",");
    }
    function cap(s) {
        return s ? s.charAt(0).toUpperCase() + s.slice(1) : "";
    }
    function count(n, word) {
        return (n === 0 ? "no" : String(n)) + " " + word + (n === 1 ? "" : "s");
    }
    function solvedText(m) {
        const s = Math.round((m.elapsedMs || 0) / 1000);
        const time = Math.floor(s / 60) + " min " + (s % 60) + " s";
        return "Solved in " + time + ", with " + count(m.mistakes || 0, "mistake")
            + " and " + count(m.hints || 0, "hint") + ".";
    }

    // Dates are the backend's local "YYYY-MM-DD"; parsed by hand (ISO parsing is UTC).
    readonly property var dayNames: ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"]
    readonly property var monthNames: ["January", "February", "March", "April", "May", "June", "July",
        "August", "September", "October", "November", "December"]
    // Daily level by weekday (KAKURO.md: as Ink Sudoku), Sunday first.
    readonly property var dailyLevels: ["expert", "easy", "easy", "medium", "medium", "hard", "hard"]
    function isoDate(d) {
        const pad = n => (n < 10 ? "0" : "") + n;
        return d.getFullYear() + "-" + pad(d.getMonth() + 1) + "-" + pad(d.getDate());
    }
    function today() {
        return isoDate(new Date());
    }
    function yesterday() {
        const d = new Date();
        d.setDate(d.getDate() - 1);
        return isoDate(d);
    }
    function parseDate(s) {
        const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(s || "");
        return m ? new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3])) : null;
    }
    function longDate(s) {
        const d = parseDate(s);
        return d ? dayNames[d.getDay()] + ", " + d.getDate() + " " + monthNames[d.getMonth()] : s;
    }
    function clock(ms) {
        const s = Math.round(ms / 1000), h = Math.floor(s / 3600), m = Math.floor(s / 60) % 60, sec = s % 60;
        const two = n => (n < 10 ? "0" : "") + n;
        return h > 0 ? h + ":" + two(m) + ":" + two(sec) : m + ":" + two(sec);
    }

    // The quiet line beside each chooser entry.
    function levelNote(level) {
        const st = stats[level] || {};
        const parts = [];
        if (st.bestMs > 0)
            parts.push("best " + clock(st.bestMs));
        if (st.streak > 0)
            parts.push("streak " + st.streak);
        if (parts.length === 0 && st.played !== undefined)
            parts.push(st.played > 0 ? "not solved yet" : "not played yet");
        return parts.join(", ");
    }
    function dailyNote() {
        const d = stats.daily || {};
        const parts = [dailyLevels[new Date().getDay()]];
        if (d.last === today())
            parts.push("solved");
        if (d.streak >= 2 && (d.last === today() || d.last === yesterday()))
            parts.push(d.streak + " days running");
        return parts.join(", ");
    }

    // ---- running head ----
    DisplayMethodArea {
        displayMethod: 3
        x: root.margin
        y: 36
        width: root.gridSize
        height: 90

        Text {
            id: headLeft
            anchors.left: parent.left
            anchors.baseline: parent.top
            anchors.baselineOffset: 62
            text: !root.puzzle ? "Kakuro"
                : root.puzzle.daily ? "Kakuro for " + root.longDate(root.puzzle.daily)
                : "Kakuro No. " + root.grouped(root.puzzle.number)
            font.family: "EB Garamond"
            font.italic: true
            font.pixelSize: 34
            font.features: ({ "onum": 1 })
            color: "black"
        }
        Text {
            anchors.horizontalCenter: parent.horizontalCenter
            anchors.baseline: headLeft.baseline
            text: [root.fingerWrites ? "finger writes" : "", root.penStream ? "pen stream" : ""]
                  .filter(s => s).join(" · ")
            font.family: "EB Garamond"
            font.italic: true
            font.pixelSize: 28
            color: "black"
        }
        Text {
            anchors.right: parent.right
            anchors.baseline: headLeft.baseline
            text: root.puzzle ? root.cap(root.puzzle.level) : ""
            font.family: "EB Garamond"
            font.italic: true
            font.pixelSize: 34
            color: "black"
        }
        MouseArea {
            width: parent.width / 2
            height: parent.height
            onPressAndHold: root.fingerWrites = !root.fingerWrites
        }
        MouseArea {
            x: parent.width / 2
            width: parent.width / 2
            height: parent.height
            onPressAndHold: root.setPenStream(!root.penStream)
        }
    }

    // ---- the grid: printed page, settled ink, live ink, input ----
    Item {
        id: grid
        x: root.margin
        y: root.gridTop
        width: root.gridSize
        height: root.gridSize

        Board {
            anchors.fill: parent
            visible: root.puzzle !== null
            n: root.n
            cells: root.puzzle && root.puzzle.cells ? root.puzzle.cells : []
            entries: root.entries
            candidates: root.candidates
            conflicts: root.conflicts
            inkRoles: root.inkRoles
            hintCells: root.hint && root.hintRevealed && root.hint.cells ? root.hint.cells : []
            selected: root.selected
        }
        SettledInk {
            id: settled
            anchors.fill: parent
            n: root.n
            thickness: live.thickness
        }
        QuietRefresh {
            id: gridQuiet
            objectName: "gridQuiet"
            anchors.fill: parent
            bleed: 40
        }
        InkLayer {
            id: live
            anchors.fill: parent
        }
        Writer {
            id: writer
            objectName: "gameWriter"
            ink: live
            gridSize: root.gridSize
            divisions: root.n
            open: root.openCells
            onBegan: cell => {
                root.lastPenAt = Date.now();
                root.undim(cell);
            }
            onTapped: cell => {
                if (root.hint)
                    root.hint = null;
                else
                    root.selected = cell === root.selected ? -1 : cell;
            }
            onSend: body => {
                root.lastPenAt = Date.now();
                root.forgetHint();
                root.firstRun = false;
                if (!root.solved)
                    root.note = "";
                root.send(root.msgInk, body);
            }
        }
        MouseArea {
            anchors.fill: parent
            preventStealing: true
            property bool writing: false
            property real downX: 0
            property real downY: 0
            property real downAt: 0
            onPressed: mouse => {
                writing = root.puzzle !== null && root.writes(mouse) && writer.down(mouse.x, mouse.y, 1);
                downX = mouse.x;
                downY = mouse.y;
                downAt = Date.now();
            }
            onPositionChanged: mouse => {
                if (writing)
                    writer.move(mouse.x, mouse.y, 1);
            }
            onReleased: mouse => {
                if (writing) {
                    writer.up();
                    writing = false;
                } else if (mouse.source === 0) {
                    // A pen tap the writer refused (in a clue cell) still dismisses a hint.
                    if (root.hint && Math.hypot(mouse.x - downX, mouse.y - downY) < writer.tapPx
                            && Date.now() - downAt < writer.tapMs)
                        root.hint = null;
                } else {
                    const c = writer.cellAt(mouse.x, mouse.y);
                    if (root.isOpen(c))
                        root.selected = c === root.selected ? -1 : c;
                }
            }
            onCanceled: {
                if (writing)
                    writer.up();
                writing = false;
            }
        }
    }

    // ---- hint, notes, actions ----
    DisplayMethodArea {
        id: foot
        displayMethod: 3
        x: root.margin
        y: root.gridTop + root.gridSize + 50
        width: root.gridSize
        height: root.height - y

        Column {
            id: footText
            width: parent.width
            y: 10
            spacing: 10
            Text {
                id: hintTitle
                visible: text !== ""
                width: parent.width
                text: !root.note && root.hint ? root.hint.title || "" : ""
                wrapMode: Text.WordWrap
                font.family: "EB Garamond"
                font.italic: true
                font.pixelSize: 54
                color: "black"
            }
            Text {
                id: hintText
                visible: text !== ""
                width: parent.width
                readonly property bool titled: !root.note && root.hint !== null && !!root.hint.title
                text: root.note || (root.hint && root.hintRevealed ? root.hint.text || "" : "")
                wrapMode: Text.WordWrap
                lineHeight: 1.0
                font.family: "EB Garamond"
                font.italic: !titled
                font.pixelSize: titled ? 42 : 46
                color: "black"
            }
        }
        // The pen tip; on a first run (no handwriting samples yet), where to teach it;
        // before the first puzzle, that one is on its way.
        Text {
            visible: hintText.text === "" && hintTitle.text === ""
            width: parent.width * 0.7
            y: 16
            text: !root.puzzle ? (root.hellos < 30 ? "Preparing a puzzle…" : "No answer from the game yet.")
                : root.firstRun ? "Teach Ink Sudoku your hand and Kakuro reads it too."
                : "Write small in a corner for a pencil mark. Scratch over any ink to erase it."
            wrapMode: Text.WordWrap
            lineHeight: 0.95
            font.family: "EB Garamond"
            font.italic: true
            font.pixelSize: 32
            color: "#555555"
        }

        Row {
            id: actions
            anchors.left: parent.left
            anchors.bottom: parent.bottom
            anchors.bottomMargin: 56
            spacing: 64
            Action { label: "New game"; onTap: root.chooserOpen = !root.chooserOpen }
            Action { label: "Hint"; onTap: root.askHint() }
            Action { label: "Undo"; enabled: root.canUndo; onTap: root.undo() }
            // Tap: quiet clean-up of the grid, no page blink. Hold: the full black flash.
            Action {
                label: "Refresh"
                onTap: { root.chooserOpen = false; root.refreshNow(); }
                onHold: { root.chooserOpen = false; flash.start(); }
            }
        }
        Action {
            anchors.right: parent.right
            anchors.bottom: actions.bottom
            label: "Close"
            onTap: root.act(() => root.close())
        }
    }

    // ---- the solved page, over the grid and foot ----
    SolvedPage {
        visible: root.solvedOpen
        y: 130
        width: root.width
        height: root.height - y
        z: 20
        info: root.solvedInfo
        level: root.puzzle ? root.puzzle.level : ""
        daily: root.puzzle !== null && !!root.puzzle.daily
        offerToday: !(root.puzzle && root.puzzle.daily === root.today())
        onNewGame: root.chooserOpen = true
        onToday: root.newDaily()
        onSeeGrid: root.solvedOpen = false
    }

    // ---- New game: a contents list just above the actions ----
    MouseArea {
        visible: root.chooserOpen
        anchors.fill: parent
        z: 50
        onClicked: root.chooserOpen = false
    }
    DisplayMethodArea {
        id: chooser
        displayMethod: 3
        visible: root.chooserOpen
        z: 50
        x: root.margin - 28
        y: root.height - 56 - 64 - 20 - height
        width: root.gridSize + 56
        height: entries_.y + entries_.height + 22

        Rectangle {
            anchors.fill: parent
            color: "white"
            border.width: 3
            border.color: "black"
        }
        MouseArea {
            anchors.fill: parent
        }
        Text {
            id: caption
            x: 28
            y: 22
            text: "New game"
            font.family: "EB Garamond"
            font.italic: true
            font.pixelSize: 30
            color: "black"
        }
        Column {
            id: entries_
            x: 28
            width: parent.width - 56
            anchors.top: caption.bottom
            anchors.topMargin: 4
            ChooserRow {
                label: "Today’s puzzle"
                note: root.dailyNote()
                current: root.puzzle !== null && root.puzzle.daily === root.today()
                onTap: root.newDaily()
            }
            Repeater {
                model: ["easy", "medium", "hard", "expert"]
                ChooserRow {
                    required property string modelData
                    label: root.cap(modelData)
                    note: root.levelNote(modelData)
                    current: root.puzzle !== null && !root.puzzle.daily && root.puzzle.level === modelData
                    onTap: root.newGame(modelData)
                }
            }
            // Kakuro has no calibration of its own: it reads the hand Ink Sudoku was taught.
            Text {
                visible: text !== ""
                width: parent.width
                topPadding: 14
                bottomPadding: 4
                text: root.personalSamples > 0
                    ? "Reads your hand from " + root.grouped(root.personalSamples) + " samples taught in Ink Sudoku."
                    : root.personalSamples === 0 ? "Teach Ink Sudoku your hand and Kakuro reads it too." : ""
                wrapMode: Text.WordWrap
                font.family: "EB Garamond"
                font.italic: true
                font.pixelSize: 30
                font.features: ({ "lnum": 1 })
                color: "#555555"
            }
        }
    }

    // Full-screen black, then white: clears e-ink ghosting.
    DisplayMethodArea {
        id: flash
        displayMethod: 3
        anchors.fill: parent
        visible: false
        z: 100
        function start() {
            root.settledUpdates = 0;
            root.refreshDue = false;
            cover.color = "black";
            visible = true;
            flashTimer.step = 0;
            flashTimer.restart();
        }
        Rectangle {
            id: cover
            anchors.fill: parent
            color: "black"
        }
        MouseArea {
            anchors.fill: parent
        }
        Timer {
            id: flashTimer
            interval: 250
            repeat: true
            property int step: 0
            onTriggered: {
                step++;
                if (step === 1) {
                    cover.color = "white";
                } else {
                    stop();
                    flash.visible = false;
                }
            }
        }
    }

    // One line of the New game list: label, dot leaders, a quiet note on the right.
    component ChooserRow: Item {
        id: row
        property string label
        property string note
        property bool current: false
        signal tap
        width: parent ? parent.width : 0
        height: 74
        Text {
            id: rowLabel
            anchors.baseline: parent.top
            anchors.baselineOffset: 50
            text: row.label
            font.family: "EB Garamond"
            font.pixelSize: 42
            font.underline: row.current
            color: "black"
        }
        Text {
            id: rowNote
            anchors.right: parent.right
            anchors.baseline: rowLabel.baseline
            text: row.note
            font.family: "EB Garamond"
            font.italic: true
            font.pixelSize: 32
            font.features: ({ "lnum": 1 })
            color: "black"
        }
        Repeater {
            readonly property real from: rowLabel.implicitWidth + 20
            readonly property real to: rowNote.x - (row.note ? 20 : 0)
            model: Math.max(0, Math.floor((to - from) / 11))
            Rectangle {
                required property int index
                x: rowNote.x - (row.note ? 20 : 0) - 2 - index * 11
                y: rowLabel.y + rowLabel.baselineOffset - 3
                width: 2
                height: 2
                color: "black"
            }
        }
        MouseArea {
            anchors.fill: parent
            onClicked: row.tap()
        }
    }
}
