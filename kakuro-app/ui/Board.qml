pragma ComponentBehavior: Bound
import QtQuick
import net.asivery.ApploadUtils 1.0

// The printed page under the ink: an n×n grid of clue cells and white cells, the clue
// sums, conflict and hint marks, the selection, and typeset stand-ins for entries and
// candidates that have no ink (set via the setCell/toggleCandidate fallback).
// Clue cells are a light halftone, not a gray: the live ink above refreshes this area
// in 1-bit modes too, and a halftone is already black and white, so it survives that
// where a flat gray would break up. A thin diagonal splits each cell that holds a sum:
// the down sum sits in the lower-left triangle, the across sum in the upper-right.
// Cells with no sum are a denser halftone without the diagonal.
Item {
    id: board

    property int n: 9
    property var cells: []          // puzzle.cells: {b, d, a}
    property var entries: []
    property var candidates: []
    property var conflicts: []
    property var inkRoles: []       // per cell: 1 = has answer ink, 2 = has candidate ink
    property var hintCells: []
    property int selected: -1
    property real bleed: 40
    readonly property real cell: width / n
    // Clue sums: relatively larger in small cells (37 px at 13×13, 47 at 9×9), capped for 7×7.
    readonly property real clueSize: Math.min(54, Math.round(12 + cell * 0.27))

    function cellX(c) { return (c % n) * cell; }
    function cellY(c) { return Math.floor(c / n) * cell; }
    function isBlack(c) { return !!(cells[c] && cells[c].b); }
    // Cell edges on whole pixels, so neighbouring fills meet exactly.
    function edge(k) { return Math.round(k * cell); }

    // The hint's cells as runs: horizontal stretches first, then vertical ones.
    function segments(list) {
        const set = {}, used = {}, out = [];
        for (const c of list)
            set[c] = true;
        const sorted = list.slice().sort((a, b) => a - b);
        for (const c of sorted) {
            if (used[c])
                continue;
            let k = c;
            while (set[k + 1] && (k + 1) % n !== 0)
                k++;
            if (k > c) {
                for (let i = c; i <= k; i++)
                    used[i] = true;
                out.push([c, k]);
            }
        }
        for (const c of sorted) {
            if (used[c])
                continue;
            let k = c;
            while (set[k + n] && !used[k + n])
                k += n;
            for (let i = c; i <= k; i += n)
                used[i] = true;
            out.push([c, k]);
        }
        return out;
    }

    DisplayMethodArea {
        x: -board.bleed
        y: -board.bleed
        width: board.width + 2 * board.bleed
        height: board.height + 2 * board.bleed
        displayMethod: 3

        Item {
            x: board.bleed
            y: board.bleed
            width: board.width
            height: board.height

            // Clue cells: halftone fills and diagonals, painted once per puzzle.
            Canvas {
                id: blocks
                anchors.fill: parent
                property var cells: board.cells
                onCellsChanged: requestPaint()
                onWidthChanged: requestPaint()
                onPaint: {
                    const ctx = getContext("2d");
                    ctx.setTransform(1, 0, 0, 1, 0, 0);
                    ctx.clearRect(0, 0, width, height);
                    const n = board.n, light = ctx.createPattern("black", Qt.Dense6Pattern),
                          dense = ctx.createPattern("black", Qt.Dense5Pattern);
                    ctx.strokeStyle = "black";
                    ctx.lineWidth = 2;
                    for (let c = 0; c < n * n && c < cells.length; c++) {
                        const k = cells[c];
                        if (!k || !k.b)
                            continue;
                        const x0 = board.edge(c % n), y0 = board.edge(Math.floor(c / n));
                        const x1 = board.edge(c % n + 1), y1 = board.edge(Math.floor(c / n) + 1);
                        const clue = k.d > 0 || k.a > 0;
                        ctx.fillStyle = clue ? light : dense;
                        ctx.fillRect(x0, y0, x1 - x0, y1 - y0);
                        if (clue) {
                            ctx.beginPath();
                            ctx.moveTo(x0, y0);
                            ctx.lineTo(x1, y1);
                            ctx.stroke();
                        }
                    }
                }
            }

            // Lines: thin between cells, heavy around the grid.
            Repeater {
                model: board.n + 1
                Rectangle {
                    required property int index
                    readonly property real w: index === 0 || index === board.n ? 6 : 2
                    x: Math.round(board.edge(index) - w / 2)
                    y: -3
                    width: w
                    height: board.height + 6
                    color: "black"
                }
            }
            Repeater {
                model: board.n + 1
                Rectangle {
                    required property int index
                    readonly property real w: index === 0 || index === board.n ? 6 : 2
                    x: -3
                    y: Math.round(board.edge(index) - w / 2)
                    width: board.width + 6
                    height: w
                    color: "black"
                }
            }

            Repeater {
                model: board.n * board.n
                Item {
                    id: cellItem
                    required property int index
                    readonly property var info: board.cells[index] || ({})
                    readonly property bool black: !!info.b
                    readonly property int down: black ? info.d || 0 : 0
                    readonly property int across: black ? info.a || 0 : 0
                    readonly property int entry: !black ? board.entries[index] || 0 : 0
                    readonly property int roles: board.inkRoles[index] || 0
                    readonly property int cands: !black && !entry && !(roles & 2) ? (board.candidates[index] || 0) : 0
                    x: board.cellX(index)
                    y: board.cellY(index)
                    width: board.cell
                    height: board.cell

                    // Clue sums: each centred in its triangle, clear of the diagonal.
                    Text {
                        visible: cellItem.down > 0
                        text: String(cellItem.down)
                        x: Math.round(board.cell * 0.3 - implicitWidth / 2)
                        anchors.baseline: parent.top
                        anchors.baselineOffset: Math.round(board.cell * 0.72 + font.pixelSize * 0.33)
                        font.family: "EB Garamond"
                        font.pixelSize: board.clueSize
                        font.weight: Font.Medium
                        font.features: ({ "lnum": 1 })
                        style: Text.Outline
                        styleColor: "white"
                        color: "black"
                    }
                    Text {
                        visible: cellItem.across > 0
                        text: String(cellItem.across)
                        x: Math.round(board.cell * 0.72 - implicitWidth / 2)
                        anchors.baseline: parent.top
                        anchors.baselineOffset: Math.round(board.cell * 0.3 + font.pixelSize * 0.33)
                        font.family: "EB Garamond"
                        font.pixelSize: board.clueSize
                        font.weight: Font.Medium
                        font.features: ({ "lnum": 1 })
                        style: Text.Outline
                        styleColor: "white"
                        color: "black"
                    }

                    // An entry with no answer ink, typeset as a stand-in.
                    Text {
                        visible: cellItem.entry > 0 && !(cellItem.roles & 1)
                        text: String(cellItem.entry)
                        anchors.horizontalCenter: parent.horizontalCenter
                        anchors.baseline: parent.top
                        anchors.baselineOffset: board.cell / 2 + font.pixelSize * 0.33
                        font.family: "EB Garamond"
                        font.italic: true
                        font.pixelSize: Math.round(board.cell * 0.62)
                        font.features: ({ "lnum": 1 })
                        color: "black"
                    }
                    Repeater {
                        model: cellItem.cands ? 9 : 0
                        Text {
                            required property int index
                            visible: cellItem.cands & (1 << index)
                            text: String(index + 1)
                            x: board.cell * (0.2 + 0.3 * (index % 3)) - width / 2
                            y: board.cell * (0.2 + 0.3 * Math.floor(index / 3)) - height / 2
                            font.family: "EB Garamond"
                            font.italic: true
                            font.pixelSize: Math.max(18, Math.round(board.cell * 0.245))
                            font.features: ({ "lnum": 1 })
                            color: "black"
                        }
                    }
                }
            }

            // Conflicts: a quiet inset outline, on white cells only.
            Repeater {
                model: board.conflicts
                Rectangle {
                    required property int modelData
                    readonly property real inset: Math.round(board.cell * 0.085)
                    visible: modelData >= 0 && modelData < board.n * board.n && !board.isBlack(modelData)
                    x: board.cellX(modelData) + inset
                    y: board.cellY(modelData) + inset
                    width: board.cell - 2 * inset
                    height: board.cell - 2 * inset
                    radius: Math.round(board.cell * 0.045)
                    color: "transparent"
                    border.width: board.cell < 110 ? 2 : 3
                    border.color: "black"
                }
            }

            // Selection (finger tap): crop marks in the corners.
            Item {
                visible: board.selected >= 0 && !board.isBlack(board.selected)
                x: board.cellX(Math.max(0, board.selected))
                y: board.cellY(Math.max(0, board.selected))
                width: board.cell
                height: board.cell
                Repeater {
                    model: 4
                    Item {
                        required property int index
                        readonly property bool atRight: index % 2 === 1
                        readonly property bool atBottom: index >= 2
                        readonly property real len: Math.round(board.cell * 0.17)
                        readonly property real inset: Math.round(board.cell * 0.07)
                        Rectangle {
                            x: parent.atRight ? board.cell - parent.inset - parent.len : parent.inset
                            y: parent.atBottom ? board.cell - parent.inset - 3 : parent.inset
                            width: parent.len
                            height: 3
                            color: "black"
                        }
                        Rectangle {
                            x: parent.atRight ? board.cell - parent.inset - 3 : parent.inset
                            y: parent.atBottom ? board.cell - parent.inset - parent.len : parent.inset
                            width: 3
                            height: parent.len
                            color: "black"
                        }
                    }
                }
            }
        }

        // Hint: a hand-drawn loop around each run of the hint's cells, inside them so it
        // stays clear of the grid lines and the neighbouring sums: a rounded superellipse
        // for a run, an ellipse for a lone cell. It overshoots a little where it closes.
        Canvas {
            id: loops
            anchors.fill: parent
            property var cells: board.hintCells
            onCellsChanged: requestPaint()
            onPaint: {
                const ctx = getContext("2d");
                ctx.setTransform(1, 0, 0, 1, 0, 0);
                ctx.clearRect(0, 0, width, height);
                ctx.translate(board.bleed, board.bleed);
                ctx.strokeStyle = "black";
                ctx.lineWidth = 4;
                ctx.lineCap = "round";
                ctx.lineJoin = "round";
                const s = board.cell;
                for (const seg of board.segments(cells || [])) {
                    const x0 = board.cellX(seg[0]), y0 = board.cellY(seg[0]);
                    const x1 = board.cellX(seg[1]) + s, y1 = board.cellY(seg[1]) + s;
                    const cx = (x0 + x1) / 2, cy = (y0 + y1) / 2;
                    const lone = seg[0] === seg[1];
                    // Half sizes: 0.42 of a cell across the run, its length less 0.08 along it.
                    const rx = lone ? s * 0.47 : x1 - x0 > s * 1.5 ? (x1 - x0) / 2 - s * 0.08 : s * 0.42;
                    const ry = lone ? s * 0.42 : y1 - y0 > s * 1.5 ? (y1 - y0) / 2 - s * 0.08 : s * 0.42;
                    const e = lone ? 1 : 0.6;
                    const a0 = -2.4 + (seg[0] % 5) * 0.15, sweep = 2 * Math.PI + 0.45;
                    const steps = 96 + Math.round((x1 - x0 + y1 - y0) / s) * 24;
                    ctx.beginPath();
                    for (let i = 0; i <= steps; i++) {
                        const f = i / steps, a = a0 + f * sweep;
                        const d = s * (-0.03 + 0.06 * f + 0.012 * Math.sin(3 * a + seg[0]));
                        const ca = Math.cos(a), sa = Math.sin(a);
                        const x = cx + Math.sign(ca) * Math.pow(Math.abs(ca), e) * (rx + d);
                        const y = cy + Math.sign(sa) * Math.pow(Math.abs(sa), e) * (ry + d);
                        if (i === 0)
                            ctx.moveTo(x, y);
                        else
                            ctx.lineTo(x, y);
                    }
                    ctx.stroke();
                }
            }
        }
    }
}
