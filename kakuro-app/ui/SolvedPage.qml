pragma ComponentBehavior: Bound
import QtQuick
import net.asivery.ApploadUtils 1.0

// The solved moment: a page laid over the grid and the foot (the running head stays),
// set like the last page of a chapter. It appears once and then stays still, so it
// lives in a Content region and may use a gray.
Rectangle {
    id: page
    color: "white"

    property var info: ({})         // the `solved` body
    property string level: ""
    property bool daily: false      // the solved game was a daily puzzle
    property bool offerToday: true
    readonly property real margin: Math.round((width - 1180) / 2)

    signal newGame
    signal today
    signal seeGrid

    readonly property var words: ["no", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten"]
    function word(n) {
        return n >= 0 && n < words.length ? words[n] : String(n);
    }
    function cap(s) {
        return s ? s.charAt(0).toUpperCase() + s.slice(1) : "";
    }
    function plural(n, w) {
        return word(n) + " " + w + (n === 1 ? "" : "s");
    }
    function duration(ms) {
        const s = Math.round((ms || 0) / 1000);
        const h = Math.floor(s / 3600), m = Math.floor(s / 60) % 60, sec = s % 60;
        const unit = (n, w) => n + " " + w + (n === 1 ? "" : "s");
        if (h > 0)
            return unit(h, "hour") + " " + unit(m, "minute");
        if (m > 0)
            return unit(m, "minute") + " " + unit(sec, "second");
        return unit(sec, "second");
    }
    function bestLine() {
        return info.best ? "A new best for " + (level || "this level") + " puzzles." : "";
    }
    function streakLine() {
        const n = info.streak || 0;
        if (n < 2)
            return "";
        return daily ? cap(word(n)) + " days running." : cap(word(n)) + " " + level + " puzzles in a row.";
    }
    function careLine() {
        const m = info.mistakes || 0, h = info.hints || 0;
        if (!m && !h)
            return "Without a mistake or a hint.";
        return "With " + plural(m, "mistake") + " and " + plural(h, "hint") + ".";
    }

    MouseArea {
        anchors.fill: parent
    }

    DisplayMethodArea {
        anchors.fill: parent
        displayMethod: 3

        Column {
            x: page.margin
            y: 250
            width: 1180
            Text {
                text: "Solved"
                font.family: "EB Garamond"
                font.pixelSize: 168
                color: "black"
            }
            Item {
                width: 1
                height: 10
            }
            Text {
                text: "in " + page.duration(page.info.elapsedMs)
                font.family: "EB Garamond"
                font.italic: true
                font.pixelSize: 60
                font.features: ({ "lnum": 1 })
                color: "black"
            }
            Item {
                width: 1
                height: 56
            }
            Rectangle {
                width: 160
                height: 3
                color: "black"
            }
            Item {
                width: 1
                height: 40
            }
            Column {
                spacing: 14
                Repeater {
                    model: [page.bestLine(), page.streakLine(), page.careLine()]
                    Text {
                        required property string modelData
                        required property int index
                        visible: modelData !== ""
                        text: modelData
                        font.family: "EB Garamond"
                        font.pixelSize: 46
                        color: index === 2 ? "#555555" : "black"
                    }
                }
            }
        }

        Row {
            x: page.margin
            y: page.height - 56 - 64
            spacing: 64
            Action {
                label: "New game"
                onTap: page.newGame()
            }
            Action {
                visible: page.offerToday
                label: "Today’s puzzle"
                onTap: page.today()
            }
        }
        Action {
            x: page.margin + 1180 - width
            y: page.height - 56 - 64
            label: "See the grid"
            onTap: page.seeGrid()
        }
    }
}
