pragma ComponentBehavior: Bound
import QtQuick
import net.asivery.ApploadUtils 1.0

// "Teach it your hand": the player writes the prompted digit in one large square box;
// each sample goes to the backend (calibrationInk, box-local px) and the current
// recognizer's read comes back. Finishing shows the before/after accuracy.
Rectangle {
    id: page
    color: "white"

    property bool fingerWrites: false
    property bool penStream: false
    readonly property real margin: Math.round((width - 1180) / 2)
    readonly property real boxSize: 720
    property alias box: box_
    property alias writer: writer_

    signal sendInk(var body)        // a calibrationInk body
    signal sendDone
    signal leave

    property string phase: "off"    // waiting | writing | finishing | result
    property int total: 27
    property int done: 0
    property int next: 0            // digit to write now; 0 = none
    property int samples: 0
    property int awaiting: 0        // id of the sample sent; no writing until `calibration` follows
    property var lastRead: null     // {digit, read}
    property var result: null       // the `personalized` body
    property string problem: ""
    property bool slow: false       // no `calibration` after a while

    function start() {
        writer_.reset();
        phase = "waiting";
        done = 0;
        next = 0;
        awaiting = 0;
        lastRead = null;
        result = null;
        problem = "";
        slow = false;
        waitTimer.restart();
    }
    function stop() {
        writer_.reset();
        waitTimer.stop();
        phase = "off";
        awaiting = 0;
    }

    // Progress comes from `calibration` alone; the backend sends one after every read.
    function onCalibration(m) {
        if (phase === "off" || phase === "finishing" || phase === "result")
            return;
        waitTimer.stop();
        awaiting = 0;
        total = m.total || total;
        done = m.done || 0;
        next = m.next || 0;
        if (m.samples !== undefined)
            samples = m.samples;
        if (next > 0)
            phase = "writing";
        else
            finish();
    }
    function onRead(m) {
        writer_.settle(m.id);
        if (phase === "off")
            return;
        problem = "";
        lastRead = { digit: m.digit, read: m.read };
        boxQuiet.run();
    }
    function onPersonalized(m) {
        if (phase === "off")
            return;
        result = m;
        if (m.samples !== undefined)
            samples = m.samples;
        phase = "result";
    }
    function onError(m) {
        if (m.id && writer_.settle(m.id) && m.id === awaiting)
            awaiting = 0;
        problem = m.message || "Something went wrong.";
    }

    function finish() {
        if (phase !== "waiting" && phase !== "writing")
            return;
        writer_.flush();
        waitTimer.stop();
        phase = "finishing";
        sendDone();
    }

    // Pen input, box-local px: from the box's MouseArea or the backend pen stream.
    function canWrite() {
        return phase === "writing" && next > 0 && awaiting === 0;
    }
    function pen(k, x, y, p, ms) {
        if (k === "down") {
            if (canWrite())
                writer_.down(x, y, p, ms);
        } else if (k === "move") {
            writer_.move(x, y, p, ms);
        } else if (k === "up") {
            writer_.up();
        }
    }

    function article(d) {
        return d === 8 ? "an" : "a";
    }
    function pct(x) {
        return Math.round(x * 100) + "%";
    }
    function readText() {
        if (problem)
            return problem;
        const r = lastRead;
        if (!r)
            return "";
        if (!r.read)
            return "That " + r.digit + " couldn’t be read, noted.";
        if (r.read === r.digit)
            return "That " + r.digit + " read as " + r.read + ".";
        return "That " + r.digit + " read as " + r.read + ", noted.";
    }
    function resultText() {
        const r = result || {};
        if (!r.samples || r.before === undefined || r.after === undefined)
            return "Nothing to learn from yet: write a few digits first.";
        return "Your digits: read right\n" + pct(r.before) + " before, " + pct(r.after) + " now.";
    }
    function resultMore() {
        const r = result || {};
        if (!r.samples)
            return "";
        return "From " + r.samples + " samples of your hand. Correcting a misread digit in the grid teaches it too.";
    }

    Timer {
        id: waitTimer
        interval: 3000
        onTriggered: page.slow = true
    }

    // The page itself: nothing under it takes input.
    MouseArea {
        anchors.fill: parent
    }

    DisplayMethodArea {
        anchors.fill: parent
        displayMethod: 3

        // Running head.
        Text {
            id: head
            x: page.margin
            anchors.baseline: parent.top
            anchors.baselineOffset: 98
            text: "Sudoku"
            font.family: "EB Garamond"
            font.italic: true
            font.pixelSize: 34
            color: "black"
        }
        Text {
            x: page.margin + 1180 - width
            anchors.baseline: head.baseline
            visible: page.phase === "writing" || page.phase === "waiting"
            text: Math.min(page.done + 1, page.total) + " of " + page.total
            font.family: "EB Garamond"
            font.italic: true
            font.pixelSize: 34
            color: "black"
        }

        Text {
            id: title
            x: page.margin - 6
            anchors.baseline: parent.top
            anchors.baselineOffset: 300
            text: "Teach it your hand"
            font.family: "EB Garamond"
            font.pixelSize: 120
            color: "black"
        }
        Text {
            x: page.margin
            y: 352
            width: 1000
            visible: page.phase !== "result"
            text: "Write each digit the way you write it in the grid; when you lift the pen, it is taken. One to nine, three times over."
            wrapMode: Text.WordWrap
            font.family: "EB Garamond"
            font.pixelSize: 40
            color: "#555555"
        }

        // The writing box, and the prompt beside it.
        Rectangle {
            x: page.margin
            y: box_.y
            width: page.boxSize
            height: page.boxSize
            visible: page.phase !== "result"
            color: "transparent"
            border.width: 3
            border.color: "black"
        }
        Item {
            id: prompt
            x: page.margin + page.boxSize + 80
            y: box_.y
            width: 1180 - page.boxSize - 80
            height: page.boxSize
            visible: page.phase !== "result"

            Text {
                id: writeA
                anchors.baseline: parent.top
                anchors.baselineOffset: 58
                visible: page.phase === "writing"
                text: "Write " + page.article(page.next)
                font.family: "EB Garamond"
                font.italic: true
                font.pixelSize: 56
                color: "black"
            }
            Text {
                anchors.baseline: parent.top
                anchors.baselineOffset: 400
                x: -10
                visible: page.phase === "writing"
                text: String(page.next)
                font.family: "EB Garamond"
                font.pixelSize: 380
                font.features: ({ "lnum": 1 })
                color: "black"
            }
            Text {
                width: parent.width
                anchors.baseline: parent.top
                anchors.baselineOffset: 58
                visible: page.phase === "waiting" || page.phase === "finishing"
                text: page.phase === "finishing" ? "Working out how well it reads you…"
                    : page.slow ? "No answer from the game yet." : "Getting ready…"
                wrapMode: Text.WordWrap
                font.family: "EB Garamond"
                font.italic: true
                font.pixelSize: 44
                color: "black"
            }
            Text {
                width: parent.width
                anchors.bottom: parent.bottom
                anchors.bottomMargin: -8
                text: page.readText()
                wrapMode: Text.WordWrap
                font.family: "EB Garamond"
                font.italic: true
                font.pixelSize: 40
                font.features: ({ "lnum": 1 })
                color: "black"
            }
        }

        // Finished: the accuracy, in words.
        Column {
            x: page.margin
            y: 420
            width: 1080
            spacing: 36
            visible: page.phase === "result"
            Text {
                width: parent.width
                text: page.resultText()
                wrapMode: Text.WordWrap
                lineHeight: 0.92
                font.family: "EB Garamond"
                font.italic: true
                font.pixelSize: 76
                font.features: ({ "lnum": 1 })
                color: "black"
            }
            Text {
                width: 980
                visible: text !== ""
                text: page.resultMore()
                wrapMode: Text.WordWrap
                font.family: "EB Garamond"
                font.pixelSize: 40
                font.features: ({ "lnum": 1 })
                color: "black"
            }
        }

        Row {
            x: page.margin
            y: page.height - 56 - 64
            spacing: 64
            Action {
                visible: page.phase === "writing" || page.phase === "waiting"
                label: "Finish"
                onTap: page.finish()
            }
            Action {
                visible: page.phase === "result"
                label: "Back to the game"
                onTap: page.leave()
            }
        }
        Action {
            x: page.margin + 1180 - width
            y: page.height - 56 - 64
            visible: page.phase !== "result"
            label: "Back to the game"
            onTap: page.leave()
        }
    }

    // Box interior: clean-up refresh, live ink, input. Box-local coordinates.
    Item {
        id: box_
        x: page.margin
        y: 530
        width: page.boxSize
        height: page.boxSize
        visible: page.phase !== "result"

        QuietRefresh {
            id: boxQuiet
            anchors.fill: parent
            anchors.margins: 4
        }
        InkLayer {
            id: live
            anchors.fill: parent
        }
        Writer {
            id: writer_
            objectName: "calibrationWriter"
            ink: live
            gridSize: page.boxSize
            divisions: 1
            restMs: 800
            onSend: body => {
                page.awaiting = body.id;
                page.sendInk({ id: body.id, digit: page.next, size: page.boxSize, strokes: body.strokes });
            }
        }
        MouseArea {
            anchors.fill: parent
            preventStealing: true
            property bool writing: false
            onPressed: mouse => {
                writing = (mouse.source === 0 ? !page.penStream : page.fingerWrites) && page.canWrite();
                if (writing)
                    page.pen("down", mouse.x, mouse.y, 1);
            }
            onPositionChanged: mouse => {
                if (writing)
                    page.pen("move", mouse.x, mouse.y, 1);
            }
            onReleased: {
                if (writing)
                    page.pen("up");
                writing = false;
            }
            onCanceled: {
                if (writing)
                    page.pen("up");
                writing = false;
            }
        }
    }
}
