import QtQuick

// The one input path. Pen samples arrive in grid-local px, from QML mouse events or
// the backend's `pen` stream alike. Writer draws them through the InkLayer, records
// them in PROTOCOL's stroke format and groups them into one `ink` message per cell:
// sent once the pen has rested `restMs`, or as soon as a stroke starts in another cell.
// A contact that stays within `tapPx` of where it landed and lifts within `tapMs` is a
// tap, not ink: its dot is erased, nothing is sent, and `tapped` fires. Only the first
// contact of a group can be a tap: once ink is waiting in the cell, a short contact is
// part of the digit (the bar of a small 5, the stub of a small 4).
Item {
    id: writer

    required property InkLayer ink
    property real gridSize: 1180
    property int divisions: 9       // cells per side; 1 makes the whole square one cell
    property int restMs: 600
    readonly property real tapPx: 6
    readonly property int tapMs: 180    // wall clock, as pen-stream times have their own epoch

    signal send(var body)           // an `ink` body, ready for the endpoint
    signal began(int cell)          // a stroke started in `cell`
    signal tapped(int cell)         // a tap in `cell` (not ink)

    property var stroke: null       // {id, cell, t0, at, x0, y0, far, points}
    property var group: null        // {cell, strokes, ids}: not sent yet
    property var pending: ({})      // ink id -> {cell, strokes, ids}: sent, awaiting `marks`
    property int nextId: 1
    readonly property bool writing: stroke !== null

    function cellAt(x, y) {
        if (x < 0 || y < 0 || x >= gridSize || y >= gridSize)
            return -1;
        const s = gridSize / divisions;
        return Math.floor(y / s) * divisions + Math.floor(x / s);
    }

    // `ms` is the sample's time in ms (any epoch); omitted, it's now.
    function down(x, y, p, ms) {
        if (stroke)
            up();
        const cell = cellAt(x, y);
        if (cell < 0)
            return false;
        rest.stop();
        if (group && group.cell !== cell)
            flush();
        if (!group)
            group = { cell: cell, strokes: [], ids: [] };
        const t0 = ms === undefined ? Date.now() : ms;
        stroke = { id: ink.down(x, y, p), cell: cell, t0: t0, at: Date.now(), x0: x, y0: y, far: 0, points: [] };
        add(x, y, p, t0);
        began(cell);
        return true;
    }
    function move(x, y, p, ms) {
        if (!stroke)
            return;
        ink.move(x, y, p);
        add(x, y, p, ms === undefined ? Date.now() : ms);
        stroke.far = Math.max(stroke.far, Math.hypot(x - stroke.x0, y - stroke.y0));
    }
    function up() {
        if (!stroke)
            return;
        ink.up();
        const s = stroke;
        stroke = null;
        if (s.far < tapPx && Date.now() - s.at < tapMs && group.strokes.length === 0) {
            ink.erase([s.id]);
            group = null;
            tapped(s.cell);
            return;
        }
        group.strokes.push(s.points);
        group.ids.push(s.id);
        rest.restart();
    }
    function add(x, y, p, ms) {
        stroke.points.push(Math.round(x * 10) / 10, Math.round(y * 10) / 10,
                           Math.max(0, Math.round(ms - stroke.t0)), Math.round(p * 1000) / 1000);
    }

    // Sends the open group now (before any action, so the backend sees ink first).
    function flush() {
        rest.stop();
        if (stroke)
            up();
        rest.stop();
        const g = group;
        group = null;
        if (!g || g.strokes.length === 0)
            return;
        const id = nextId++;
        pending[id] = g;
        send({ id: id, cell: g.cell, gridSize: gridSize, strokes: g.strokes });
    }

    // `marks` arrived for ink `id`: its live strokes go (the settled layer draws the
    // cell now). Returns what was sent, or null.
    function settle(id) {
        const g = pending[id];
        if (!g)
            return null;
        ink.erase(g.ids);
        delete pending[id];
        return g;
    }
    // Erases live ink of everything sent but not yet answered (after `error` or a
    // full `state`); late `marks` still settle normally.
    function dropPending() {
        for (const id in pending)
            ink.erase(pending[id].ids);
    }
    // New game: forget everything, sent or not.
    function reset() {
        rest.stop();
        stroke = null;
        group = null;
        pending = {};
        ink.clear();
    }

    Timer {
        id: rest
        interval: writer.restMs
        onTriggered: writer.flush()
    }
}
