import QtQuick
import net.asivery.ApploadUtils 1.0

// Live ink: puts the stroke under the pen on the panel as fast as possible, and
// nothing more. Coordinates are this item's px (the grid's); ink may run `bleed` px
// past its edges. Strokes stay until erase()/clear(), so the game can swap them for
// settled ink once the backend has them.
//
//   down(x, y, p) -> stroke id     move(x, y, p)     up() -> stroke id
//   erase(ids)                     clear()
//
// How it draws (rotated Rectangle pieces in a UFast region) is private: swap the
// technique or the display method freely, keep the API.
Item {
    id: layer

    property real thickness: 5
    property real bleed: 40
    // Display method as a plain int (0 UFast, 1 Fast, 2 Animate, 3 Content, 4 UI):
    // the on-device item is patched and its enum may not survive.
    property int displayMethod: 0

    property int nextId: 1
    property Item stroke: null
    property int strokeId: 0
    property var strokes: ({})   // id -> container Item
    property real lastX: 0
    property real lastY: 0

    function down(x, y, p) {
        if (stroke)
            up();
        const id = nextId++;
        stroke = strokeItem.createObject(pieces);
        strokeId = id;
        strokes[id] = stroke;
        lastX = x;
        lastY = y;
        piece(x, y, x, y);
        return id;
    }
    function move(x, y, p) {
        if (!stroke)
            return;
        const dx = x - lastX, dy = y - lastY;
        if (dx * dx + dy * dy < 2.25)
            return;
        piece(lastX, lastY, x, y);
        lastX = x;
        lastY = y;
    }
    function up() {
        const id = strokeId;
        stroke = null;
        strokeId = 0;
        return id;
    }
    function erase(ids) {
        for (const id of ids) {
            const s = strokes[id];
            if (!s)
                continue;
            if (s === stroke) {
                stroke = null;
                strokeId = 0;
            }
            s.destroy();
            delete strokes[id];
        }
    }
    function clear() {
        stroke = null;
        strokeId = 0;
        for (const id in strokes)
            strokes[id].destroy();
        strokes = {};
    }

    // A thickness-tall bar centred on the segment, rotated onto it; its overhang of
    // thickness/2 at each end covers the joints.
    function piece(x0, y0, x1, y1) {
        const dx = x1 - x0, dy = y1 - y0, t = thickness;
        const len = Math.sqrt(dx * dx + dy * dy);
        bar.createObject(stroke, {
            x: (x0 + x1 - len - t) / 2,
            y: (y0 + y1 - t) / 2,
            width: len + t,
            height: t,
            rotation: Math.atan2(dy, dx) * 180 / Math.PI
        });
    }

    DisplayMethodArea {
        x: -layer.bleed
        y: -layer.bleed
        width: layer.width + 2 * layer.bleed
        height: layer.height + 2 * layer.bleed
        displayMethod: layer.displayMethod
        Item {
            id: pieces
            x: layer.bleed
            y: layer.bleed
        }
    }
    Component {
        id: strokeItem
        Item {}
    }
    Component {
        id: bar
        Rectangle {
            color: "black"
            antialiasing: false
        }
    }
}
