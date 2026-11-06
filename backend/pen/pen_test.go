package pen

import (
	"reflect"
	"testing"
)

type raw struct {
	typ, code uint16
	value     int32
}

func decode(frames ...[]raw) []Kind {
	var d decoder
	var kinds []Kind
	for i, f := range frames {
		for _, r := range f {
			d.feed(int64(i), 0, r.typ, r.code, r.value, func(e Event) { kinds = append(kinds, e.Kind) })
		}
		d.feed(int64(i), 0, evSyn, synReport, 0, func(e Event) { kinds = append(kinds, e.Kind) })
	}
	return kinds
}

func TestStrokeLifecycle(t *testing.T) {
	got := decode(
		[]raw{{evKey, btnToolPen, 1}, {evAbs, absX, 10}},
		[]raw{{evAbs, absX, 11}},
		[]raw{{evKey, btnTouch, 1}, {evAbs, absPressure, 900}},
		[]raw{{evAbs, absY, 5}},
		[]raw{{evKey, btnTouch, 0}},
		[]raw{{evKey, btnToolPen, 0}},
	)
	want := []Kind{Enter, Hover, Down, Move, Up, Leave}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// A fast pen can enter range and touch down within one frame, and lift and
// leave within one frame; Down and Up must still be reported.
func TestCollapsedTransitions(t *testing.T) {
	got := decode(
		[]raw{{evKey, btnToolPen, 1}, {evKey, btnTouch, 1}},
		[]raw{{evAbs, absX, 3}},
		[]raw{{evKey, btnTouch, 0}, {evKey, btnToolPen, 0}},
	)
	want := []Kind{Enter, Down, Move, Up, Leave}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestEraserLeaveKeepsTool(t *testing.T) {
	var d decoder
	var last Event
	emit := func(e Event) { last = e }
	d.feed(0, 0, evKey, btnToolRubber, 1, emit)
	d.feed(0, 0, evSyn, synReport, 0, emit)
	d.feed(1, 0, evKey, btnToolRubber, 0, emit)
	d.feed(1, 0, evSyn, synReport, 0, emit)
	if last.Kind != Leave || last.Tool != ToolEraser {
		t.Fatalf("got %v/%v, want leave/eraser", last.Kind, last.Tool)
	}
}
