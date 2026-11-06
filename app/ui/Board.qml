pragma ComponentBehavior: Bound
import QtQuick
import net.asivery.ApploadUtils 1.0

// The printed page under the ink: grid, givens, conflict and hint marks, the
// selection, answer dots, and typeset stand-ins for entries/candidates that have no ink (set via
// the setCell/toggleCandidate fallback). Everything is black: the ink layers above
// refresh this area in 1-bit modes too, and grays would not survive that.
Item {
    id: board

    property string givens: ""
    property string entries: ""
    property var candidates: []
    property var conflicts: []
    property var inkRoles: []       // per cell: 1 = has answer ink, 2 = has candidate ink
    property var hintCells: []
    property int selected: -1
    property real bleed: 40
    readonly property real cell: width / 9

    function cellX(c) { return (c % 9) * cell; }
    function cellY(c) { return Math.floor(c / 9) * cell; }

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

            // Lines: thin between cells, heavy around boxes.
            Repeater {
                model: 10
                Rectangle {
                    required property int index
                    readonly property real w: index % 3 === 0 ? 6 : 2
                    x: Math.round(index * board.cell - w / 2)
                    y: -3
                    width: w
                    height: board.height + 6
                    color: "black"
                }
            }
            Repeater {
                model: 10
                Rectangle {
                    required property int index
                    readonly property real w: index % 3 === 0 ? 6 : 2
                    x: -3
                    y: Math.round(index * board.cell - w / 2)
                    width: board.width + 6
                    height: w
                    color: "black"
                }
            }

            Repeater {
                model: 81
                Item {
                    id: cellItem
                    required property int index
                    readonly property string given: board.givens.length === 81 && board.givens[index] !== "." ? board.givens[index] : ""
                    readonly property string entry: !given && board.entries.length === 81 && board.entries[index] !== "." ? board.entries[index] : ""
                    readonly property int roles: board.inkRoles[index] || 0
                    readonly property int cands: !given && !entry && !(roles & 2) ? (board.candidates[index] || 0) : 0
                    x: board.cellX(index)
                    y: board.cellY(index)
                    width: board.cell
                    height: board.cell

                    Text {
                        visible: cellItem.given !== ""
                        text: cellItem.given
                        anchors.horizontalCenter: parent.horizontalCenter
                        anchors.baseline: parent.top
                        anchors.baselineOffset: board.cell / 2 + font.pixelSize * 0.33
                        font.family: "EB Garamond"
                        font.pixelSize: 84
                        font.weight: Font.Medium
                        font.features: ({ "lnum": 1 })
                        color: "black"
                    }
                    Text {
                        visible: cellItem.entry !== "" && !(cellItem.roles & 1)
                        text: cellItem.entry
                        anchors.horizontalCenter: parent.horizontalCenter
                        anchors.baseline: parent.top
                        anchors.baselineOffset: board.cell / 2 + font.pixelSize * 0.33
                        font.family: "EB Garamond"
                        font.italic: true
                        font.pixelSize: 84
                        font.features: ({ "lnum": 1 })
                        color: "black"
                    }
                    // Counted as an answer: a small dot top right, so a digit read as a
                    // pencil mark (no dot) stands out.
                    Rectangle {
                        visible: cellItem.entry !== ""
                        x: board.cell - 24
                        y: 16
                        width: 8
                        height: 8
                        radius: 4
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
                            font.pixelSize: 32
                            font.features: ({ "lnum": 1 })
                            color: "black"
                        }
                    }
                }
            }

            // Conflicts: a quiet inset outline (givens too, if the backend lists them).
            Repeater {
                model: board.conflicts
                Rectangle {
                    required property int modelData
                    x: board.cellX(modelData) + 11
                    y: board.cellY(modelData) + 11
                    width: board.cell - 22
                    height: board.cell - 22
                    radius: 6
                    color: "transparent"
                    border.width: 3
                    border.color: "black"
                }
            }

            // Selection (finger tap): crop marks in the corners.
            Item {
                visible: board.selected >= 0
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
                        readonly property real len: 22
                        readonly property real inset: 9
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

        // Hint: a hand-drawn loop around each of the hint's cells.
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
                for (const c of cells || []) {
                    const cx = board.cellX(c) + board.cell / 2, cy = board.cellY(c) + board.cell / 2;
                    const rx = board.cell * 0.54, ry = board.cell * 0.46;
                    const a0 = -2.4 + (c % 5) * 0.15, sweep = 2 * Math.PI + 0.55;
                    ctx.beginPath();
                    for (let i = 0; i <= 72; i++) {
                        const f = i / 72, a = a0 + f * sweep;
                        const k = 0.94 + 0.1 * f + 0.025 * Math.sin(3 * a + c);
                        const x = cx + Math.cos(a) * rx * k, y = cy + Math.sin(a) * ry * k;
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
