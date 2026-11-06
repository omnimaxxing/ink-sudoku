pragma ComponentBehavior: Bound
import QtQuick
import net.asivery.ApploadUtils 1.0

// Ink the backend has accepted (state.ink, marks.cellState.ink): one Canvas per cell,
// in a Content region. It appears exactly where the live ink already is, so quality
// matters more than speed here. Strokes with a role other than answer/candidate, and
// the `dim` strokes, are drawn dotted: a 1-bit-safe "dimmed" for unknown reads.
Item {
    id: settled

    property real thickness: 5
    property real candidateThickness: 3 // pencil marks settle thinner: how the ink was read
    property real bleed: 40
    property int displayMethod: 3
    property bool finished: false   // solved: draw answer ink only (no pencil marks or dotted ink)
    readonly property real cell: width / 9

    // strokes: [{role, points}], dim: [points]
    function setCell(c, strokes, dim) {
        const cv = cells.itemAt(c);
        if (!cv)
            return;
        cv.strokes = strokes || [];
        cv.dim = dim || [];
    }
    // ink: {"cell": [{role, points}]}, dim: {cell: [points]}
    function setAll(ink, dim) {
        for (let c = 0; c < 81; c++)
            setCell(c, ink[String(c)], dim[c]);
    }

    DisplayMethodArea {
        x: -settled.bleed
        y: -settled.bleed
        width: settled.width + 2 * settled.bleed
        height: settled.height + 2 * settled.bleed
        displayMethod: settled.displayMethod

        Repeater {
            id: cells
            model: 81
            Canvas {
                id: cv
                required property int index
                property var strokes: []
                property var dim: []
                // Grid-local position of this canvas's top-left corner.
                readonly property real ox: (index % 9) * settled.cell - settled.bleed
                readonly property real oy: Math.floor(index / 9) * settled.cell - settled.bleed
                x: ox + settled.bleed
                y: oy + settled.bleed
                width: settled.cell + 2 * settled.bleed
                height: settled.cell + 2 * settled.bleed
                visible: strokes.length > 0 || dim.length > 0
                onStrokesChanged: requestPaint()
                onDimChanged: requestPaint()
                readonly property bool finished: settled.finished
                onFinishedChanged: requestPaint()

                onPaint: {
                    const ctx = getContext("2d");
                    ctx.setTransform(1, 0, 0, 1, 0, 0);
                    ctx.clearRect(0, 0, width, height);
                    ctx.translate(-ox, -oy);
                    ctx.strokeStyle = "black";
                    ctx.fillStyle = "black";
                    ctx.lineWidth = settled.thickness;
                    ctx.lineCap = "round";
                    ctx.lineJoin = "round";
                    for (const s of strokes) {
                        if (settled.finished && s.role !== "answer")
                            continue;
                        ctx.lineWidth = s.role === "candidate" ? settled.candidateThickness : settled.thickness;
                        if (s.role === "answer" || s.role === "candidate")
                            solid(ctx, s.points);
                        else
                            dotted(ctx, s.points);
                    }
                    for (const pts of settled.finished ? [] : dim)
                        dotted(ctx, pts);
                }
                function solid(ctx, pts) {
                    if (pts.length < 4)
                        return;
                    ctx.beginPath();
                    ctx.moveTo(pts[0], pts[1]);
                    if (pts.length === 4)
                        ctx.lineTo(pts[0] + 0.1, pts[1]);
                    for (let i = 4; i + 1 < pts.length; i += 4)
                        ctx.lineTo(pts[i], pts[i + 1]);
                    ctx.stroke();
                }
                // Dots every `gap` px along the stroke.
                function dotted(ctx, pts) {
                    const gap = 8, r = settled.thickness * 0.45;
                    ctx.beginPath();
                    let carry = 0;
                    for (let i = 0; i + 1 < pts.length; i += 4) {
                        const x0 = pts[i], y0 = pts[i + 1];
                        if (i === 0) {
                            ctx.moveTo(x0 + r, y0);
                            ctx.arc(x0, y0, r, 0, 2 * Math.PI);
                            continue;
                        }
                        const px = pts[i - 4], py = pts[i - 3];
                        const len = Math.hypot(x0 - px, y0 - py);
                        let d = gap - carry;
                        while (d <= len) {
                            const x = px + (x0 - px) * d / len, y = py + (y0 - py) * d / len;
                            ctx.moveTo(x + r, y);
                            ctx.arc(x, y, r, 0, 2 * Math.PI);
                            d += gap;
                        }
                        carry = len - (d - gap);
                    }
                    ctx.fill();
                }
            }
        }
    }
}
