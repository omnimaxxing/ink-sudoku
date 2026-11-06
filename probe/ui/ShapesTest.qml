import QtQuick
import QtQuick.Shapes

// Loaded through a Loader so a missing or non-drawing Shapes module can't
// break the rest of the probe.
Shape {
    ShapePath {
        strokeWidth: 4
        strokeColor: "black"
        fillColor: "transparent"
        startX: 0; startY: 80
        PathLine { x: 50; y: 0 }
        PathLine { x: 100; y: 80 }
        PathLine { x: 0; y: 80 }
    }
}
