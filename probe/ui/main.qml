import QtQuick
import net.asivery.AppLoad 1.0
import net.asivery.ApploadUtils 1.0

// G1 probe: what draws, how fast ink appears under each display method, and
// how xochitl's mouse events compare with reading the pen ourselves.
Rectangle {
    id: root
    anchors.fill: parent
    color: "white"

    signal close
    function unloading() {
        endpoint.terminate();
    }

    // Message types; keep in sync with backend/cmd/probe/main.go.
    readonly property int msgInfo: 1
    readonly property int msgPen: 2
    readonly property int msgStats: 3
    readonly property int msgResults: 10
    readonly property int msgSaved: 11

    property string inkSource: "xochitl"  // which path draws ink: "xochitl" or "backend"
    property var dev: null
    property string status: "waiting for backend…"

    property var kernelLat: []   // ms from kernel timestamp to our backend reading it
    property var socketLat: []   // ms from our backend reading it to arrival here
    property var xochitlLat: []  // ms from kernel pen-down to xochitl's mouse press arriving here
    property var downs: []       // recent backend pen-downs: {ms, x, y}
    property var pairs: []       // calibration: [rawX, rawY, screenX, screenY]
    property var fit: null       // raw -> root coordinates: {ax, bx, ay, by, rms, swapped}
    property int mouseMoves: 0
    property int mouseRate: 0
    property int backendRate: 0
    property int maxPressure: 0
    property int lastSource: -1
    property var backendPanel: null

    AppLoad {
        id: endpoint
        applicationID: "sudoku-probe"
        onMessageReceived: (type, contents) => root.onBackend(type, contents)
    }

    function onBackend(type, contents) {
        const m = JSON.parse(contents);
        if (type === msgInfo) {
            dev = m;
            status = m.ok ? "pen: " + m.name + " (" + m.path + ")" : "pen error: " + m.error;
        } else if (type === msgPen) {
            onPen(m);
        } else if (type === msgStats) {
            backendRate = m.rate;
            maxPressure = m.maxPressure;
        } else if (type === msgSaved) {
            status = m.status;
        }
    }

    function onPen(e) {
        const now = Date.now();
        push(kernelLat, (e.rx - e.ts) / 1000, 4000);
        push(socketLat, now - e.rx / 1000, 4000);
        if (e.k === "down")
            push(downs, { ms: e.ts / 1000, x: e.x, y: e.y }, 20);
        if (inkSource !== "backend")
            return;
        const p = toRoot(e.x, e.y);
        if (e.k === "down") {
            backendPanel = panelAt(p.x, p.y);
            if (backendPanel) {
                const q = backendPanel.mapFromItem(root, p.x, p.y);
                backendPanel.down(q.x, q.y);
            }
        } else if (e.k === "move" && backendPanel) {
            const q = backendPanel.mapFromItem(root, p.x, p.y);
            backendPanel.move(q.x, q.y);
        } else if (e.k === "up" && backendPanel) {
            backendPanel.up();
            backendPanel = null;
        }
    }

    function push(arr, v, cap) {
        arr.push(v);
        if (arr.length > cap)
            arr.shift();
    }

    // Raw digitizer units -> root coordinates: the fitted map once we have
    // calibration pairs, else a naive range-based guess.
    function toRoot(x, y) {
        if (fit) {
            const sx = fit.swapped ? y : x, sy = fit.swapped ? x : y;
            return { x: fit.ax * sx + fit.bx, y: fit.ay * sy + fit.by };
        }
        const r = dev ? dev.ranges : null;
        if (!r)
            return { x: 0, y: 0 };
        return {
            x: (x - r.X.Min) / (r.X.Max - r.X.Min) * root.width,
            y: (y - r.Y.Min) / (r.Y.Max - r.Y.Min) * root.height
        };
    }

    function panelAt(x, y) {
        for (let i = 0; i < panels.count; i++) {
            const p = panels.itemAt(i);
            const q = p.mapFromItem(root, x, y);
            if (q.x >= 0 && q.y >= 0 && q.x < p.width && q.y < p.height)
                return p;
        }
        return null;
    }

    // Least squares s = a·r + b for one axis; returns {a, b, rms}.
    function line(rs, ss) {
        const n = rs.length;
        let mr = 0, ms = 0;
        for (let i = 0; i < n; i++) { mr += rs[i]; ms += ss[i]; }
        mr /= n; ms /= n;
        let cov = 0, vr = 0;
        for (let i = 0; i < n; i++) { cov += (rs[i] - mr) * (ss[i] - ms); vr += (rs[i] - mr) ** 2; }
        const a = vr > 0 ? cov / vr : 0, b = ms - a * mr;
        let e = 0;
        for (let i = 0; i < n; i++) e += (a * rs[i] + b - ss[i]) ** 2;
        return { a: a, b: b, rms: Math.sqrt(e / n) };
    }

    function refit() {
        if (pairs.length < 4)
            return;
        const rx = pairs.map(p => p[0]), ry = pairs.map(p => p[1]);
        const sx = pairs.map(p => p[2]), sy = pairs.map(p => p[3]);
        const fx = line(rx, sx), fy = line(ry, sy);
        const gx = line(ry, sx), gy = line(rx, sy);
        const swapped = gx.rms + gy.rms < fx.rms + fy.rms;
        const ax = swapped ? gx : fx, ay = swapped ? gy : fy;
        fit = { ax: ax.a, bx: ax.b, ay: ay.a, by: ay.b, rms: Math.max(ax.rms, ay.rms), swapped: swapped };
    }

    // xochitl's mouse press: match it to the backend pen-down it came from.
    function onMouseDown(panel, mouse) {
        const now = Date.now();
        lastSource = mouse.source;
        let best = null;
        for (const d of downs)
            if (now - d.ms >= 0 && now - d.ms < 500 && (!best || d.ms > best.ms))
                best = d;
        if (best) {
            push(xochitlLat, now - best.ms, 500);
            const p = panel.mapToItem(root, mouse.x, mouse.y);
            push(pairs, [best.x, best.y, p.x, p.y], 200);
            refit();
        }
        if (inkSource === "xochitl")
            panel.down(mouse.x, mouse.y);
    }
    function onMouseMove(panel, mouse) {
        mouseMoves++;
        if (inkSource === "xochitl")
            panel.move(mouse.x, mouse.y);
    }
    function onMouseUp(panel, mouse) {
        if (inkSource === "xochitl")
            panel.up();
    }

    function summary(arr) {
        if (arr.length === 0)
            return "–";
        const s = arr.slice().sort((a, b) => a - b);
        const q = f => s[Math.min(s.length - 1, Math.floor(f * s.length))].toFixed(1);
        return "med " + q(0.5) + " · p95 " + q(0.95) + " ms (n=" + s.length + ")";
    }

    function save() {
        const sorted = a => a.slice().sort((x, y) => x - y);
        endpoint.sendMessage(msgResults, JSON.stringify({
            time: new Date().toISOString(),
            window: [root.width, root.height],
            dev: dev,
            inkSource: inkSource,
            backendRate: backendRate,
            maxPressure: maxPressure,
            lastSource: lastSource,
            kernelLat: summary(kernelLat),
            socketLat: summary(socketLat),
            xochitlLat: summary(xochitlLat),
            xochitlLatRaw: sorted(xochitlLat),
            fit: fit,
            pairs: pairs.length,
            pieces: Array.from({ length: panels.count }, (_, i) => [panels.itemAt(i).label, panels.itemAt(i).pieces])
        }));
    }

    Timer {
        interval: 1000
        running: true
        repeat: true
        onTriggered: {
            root.mouseRate = root.mouseMoves;
            root.mouseMoves = 0;
            stats.text = root.statsText();
        }
    }

    function statsText() {
        const f = fit ? "fit ax " + fit.ax.toFixed(4) + " bx " + fit.bx.toFixed(1)
                        + " ay " + fit.ay.toFixed(4) + " by " + fit.by.toFixed(1)
                        + " rms " + fit.rms.toFixed(1) + "px" + (fit.swapped ? " SWAPPED" : "")
                      : "fit: write ≥4 pen-downs";
        return "window " + root.width + "×" + root.height
            + "   pen ranges " + (dev && dev.ok ? "x " + dev.ranges.X.Max + " y " + dev.ranges.Y.Max + " p " + dev.ranges.Pressure.Max : "–")
            + "   max pressure seen " + maxPressure
            + "\nbackend: " + backendRate + " ev/s · kernel→backend " + summary(kernelLat)
            + " · backend→QML " + summary(socketLat)
            + "\nxochitl: " + mouseRate + " moves/s · pen-down→press " + summary(xochitlLat)
            + " · last mouse.source " + lastSource
            + "\n" + f;
    }

    // ---- layout ----
    readonly property real m: 16
    readonly property real barH: 300
    readonly property real stripH: 190

    Column {
        x: m
        y: m
        width: root.width - 2 * m
        spacing: 8
        Row {
            spacing: 12
            Btn { label: "Ink: " + root.inkSource; onTap: root.inkSource = root.inkSource === "xochitl" ? "backend" : "xochitl" }
            Btn { label: "Clear"; onTap: { for (let i = 0; i < panels.count; i++) panels.itemAt(i).clear(); } }
            Btn { label: "Flash"; onTap: flash.start() }
            Btn { label: "Save"; onTap: root.save() }
            Btn { label: "Close"; onTap: root.close() }
        }
        Text { text: root.status; font.pixelSize: 22 }
        Text { id: stats; font.pixelSize: 22; text: root.statsText() }
    }

    Grid {
        id: grid
        x: m
        y: barH
        columns: 3
        spacing: m
        readonly property real cellW: (root.width - 4 * m) / 3
        readonly property real cellH: (root.height - barH - stripH - 3 * m) / 2
        Repeater {
            id: panels
            // Display methods as plain ints (0 UFast, 1 Fast, 2 Animate, 3 Content,
            // 4 UI): the on-device patch indexes a list with this value, and the
            // enum may not survive it.
            model: [
                { label: "UFast", method: 0, canvas: false },
                { label: "Fast", method: 1, canvas: false },
                { label: "Animate", method: 2, canvas: false },
                { label: "Content", method: 3, canvas: false },
                { label: "UI", method: 4, canvas: false },
                { label: "UFast canvas", method: 0, canvas: true }
            ]
            InkPanel {
                width: grid.cellW
                height: grid.cellH
                label: modelData.label
                displayMethod: modelData.method
                useCanvas: modelData.canvas
                onMouseDown: (p, mouse) => root.onMouseDown(p, mouse)
                onMouseMove: (p, mouse) => root.onMouseMove(p, mouse)
                onMouseUp: (p, mouse) => root.onMouseUp(p, mouse)
            }
        }
    }

    // What renders at all: rotated Rectangle, Shapes path, text sizes, grays.
    DisplayMethodArea {
        displayMethod: 3 // Content
        x: m
        y: root.height - stripH
        width: root.width - 2 * m
        height: stripH - m
        Row {
            anchors.verticalCenter: parent.verticalCenter
            spacing: 40
            Rectangle { width: 100; height: 20; color: "black"; rotation: 30; antialiasing: false }
            Loader {
                id: shapes
                width: 100
                height: 80
                source: "ShapesTest.qml"
            }
            Text { text: shapes.status === Loader.Ready ? "Shapes loaded" : "Shapes: status " + shapes.status; font.pixelSize: 20 }
            Repeater {
                model: ["#000000", "#555555", "#aaaaaa", "#dddddd"]
                Rectangle { width: 70; height: 70; color: modelData; border.width: 1 }
            }
            Column {
                Text { text: "Sudoku 9"; font.pixelSize: 64 }
                Text { text: "pencil 1 2 3"; font.pixelSize: 20 }
            }
        }
    }

    // Full-screen black/white flash to clear ghosting.
    DisplayMethodArea {
        id: flash
        displayMethod: 3 // Content
        anchors.fill: parent
        visible: false
        z: 100
        function start() { visible = true; cover.color = "black"; flashTimer.step = 0; flashTimer.start(); }
        Rectangle { id: cover; anchors.fill: parent; color: "black" }
        Timer {
            id: flashTimer
            interval: 250
            repeat: true
            property int step: 0
            onTriggered: {
                step++;
                if (step === 1) cover.color = "white";
                else { stop(); flash.visible = false; }
            }
        }
    }

    component Btn: Rectangle {
        property string label
        signal tap
        width: 230
        height: 90
        border.width: 3
        border.color: "black"
        Text { anchors.centerIn: parent; text: parent.label; font.pixelSize: 28 }
        MouseArea { anchors.fill: parent; onClicked: parent.tap() }
    }
}
