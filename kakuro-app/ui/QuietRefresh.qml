import QtQuick
import net.asivery.ApploadUtils 1.0

// A quiet quality refresh of one region, lighter than the full-screen black flash:
// a white sheet in a Content region covers it for one beat, then lifts, so the whole
// region is redrawn in Content mode. Put it above the layers to clean and below live
// ink, so a stroke started meanwhile still shows.
Item {
    id: quiet

    property real bleed: 0
    property int holdMs: 300
    readonly property bool running: area.visible

    function run() {
        area.visible = true;
        hold.restart();
    }

    DisplayMethodArea {
        id: area
        x: -quiet.bleed
        y: -quiet.bleed
        width: quiet.width + 2 * quiet.bleed
        height: quiet.height + 2 * quiet.bleed
        displayMethod: 3
        visible: false
        Rectangle {
            anchors.fill: parent
            color: "white"
        }
    }
    Timer {
        id: hold
        interval: quiet.holdMs
        onTriggered: area.visible = false
    }
}
