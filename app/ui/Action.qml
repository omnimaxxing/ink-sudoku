import QtQuick

// A plain-text action with a generous touch target.
Item {
    id: action
    property string label
    property bool current: false
    signal tap
    signal hold     // press and hold; unhandled, it does nothing (the tap doesn't fire either)
    width: label_.implicitWidth
    height: 64
    Text {
        id: label_
        anchors.baseline: parent.top
        anchors.baselineOffset: 46
        text: action.label
        font.family: "EB Garamond"
        font.pixelSize: 42
        font.underline: action.current
        color: action.enabled ? "black" : "#999999"
    }
    MouseArea {
        anchors.fill: parent
        anchors.margins: -24
        enabled: action.enabled
        onClicked: action.tap()
        onPressAndHold: action.hold()
    }
}
