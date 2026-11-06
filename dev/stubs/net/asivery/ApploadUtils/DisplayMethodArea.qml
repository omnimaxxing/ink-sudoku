import QtQuick

// Mac stand-in: on the tablet this becomes xochitl's e-ink region item.
Item {
    enum Method { UFast, Fast, Animate, Content, UI }
    property int displayMethod: DisplayMethodArea.Content
}
