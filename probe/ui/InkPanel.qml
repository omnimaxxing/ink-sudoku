import QtQuick
import net.asivery.ApploadUtils 1.0

// One test panel: draws pen ink under one e-ink display method, either as
// rotated Rectangle pieces (one per segment) or into a Canvas.
DisplayMethodArea {
    id: panel

    property string label
    property bool useCanvas: false
    property int pieces: 0
    property real thickness: 4

    // Mouse events (xochitl's path) are reported to the root, which decides
    // whether they draw.
    signal mouseDown(var panel, var mouse)
    signal mouseMove(var panel, var mouse)
    signal mouseUp(var panel, var mouse)

    property real lastX: 0
    property real lastY: 0
    property bool drawing: false

    function down(x, y) {
        drawing = true;
        lastX = x;
        lastY = y;
        segment(x, y, x + 0.01, y);
    }
    function move(x, y) {
        if (!drawing)
            return;
        segment(lastX, lastY, x, y);
        lastX = x;
        lastY = y;
    }
    function up() {
        drawing = false;
    }
    function segment(x0, y0, x1, y1) {
        pieces++;
        if (useCanvas) {
            canvas.queue.push([x0, y0, x1, y1]);
            const t = thickness;
            canvas.markDirty(Qt.rect(Math.min(x0, x1) - t, Math.min(y0, y1) - t,
                                     Math.abs(x1 - x0) + 2 * t, Math.abs(y1 - y0) + 2 * t));
            return;
        }
        // A thickness-tall bar centred on the segment's midpoint, rotated onto it.
        const dx = x1 - x0, dy = y1 - y0;
        const len = Math.sqrt(dx * dx + dy * dy);
        piece.createObject(layer, {
            x: (x0 + x1 - len - thickness) / 2,
            y: (y0 + y1 - thickness) / 2,
            width: len + thickness,
            height: thickness,
            rotation: Math.atan2(dy, dx) * 180 / Math.PI
        });
    }
    function clear() {
        for (let i = layer.children.length - 1; i >= 0; i--)
            layer.children[i].destroy();
        pieces = 0;
        canvas.queue = [];
        canvas.wipe = true;
        canvas.requestPaint();
    }

    Rectangle {
        anchors.fill: parent
        color: "transparent"
        border.width: 2
        border.color: "black"
    }
    Text {
        x: 10
        y: 6
        text: panel.label + "  ·  " + panel.pieces
        font.pixelSize: 22
    }

    Item {
        id: layer
        anchors.fill: parent
        clip: true
    }
    Component {
        id: piece
        Rectangle {
            color: "black"
            antialiasing: false
        }
    }
    Canvas {
        id: canvas
        anchors.fill: parent
        visible: panel.useCanvas
        property var queue: []
        property bool wipe: false
        onPaint: {
            const ctx = getContext("2d");
            if (wipe) {
                ctx.clearRect(0, 0, width, height);
                wipe = false;
            }
            ctx.lineWidth = panel.thickness;
            ctx.lineCap = "round";
            ctx.strokeStyle = "black";
            ctx.beginPath();
            for (const s of queue) {
                ctx.moveTo(s[0], s[1]);
                ctx.lineTo(s[2], s[3]);
            }
            ctx.stroke();
            queue = [];
        }
    }

    MouseArea {
        anchors.fill: parent
        onPressed: (mouse) => panel.mouseDown(panel, mouse)
        onPositionChanged: (mouse) => panel.mouseMove(panel, mouse)
        onReleased: (mouse) => panel.mouseUp(panel, mouse)
    }
}
