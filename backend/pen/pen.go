// Package pen reads the stylus straight from its Linux evdev node, in parallel
// with xochitl (no exclusive grab), so the app sees every sample at full rate
// with pressure, tilt and the eraser end.
package pen

// Linux input event codes used by the digitizer.
const (
	evSyn = 0x00
	evKey = 0x01
	evAbs = 0x03

	synReport = 0x00

	btnToolPen    = 0x140
	btnToolRubber = 0x141
	btnTouch      = 0x14a

	absX        = 0x00
	absY        = 0x01
	absPressure = 0x18
	absDistance = 0x19
	absTiltX    = 0x1a
	absTiltY    = 0x1b
)

type Tool uint8

const (
	ToolNone Tool = iota
	ToolPen
	ToolEraser
)

func (t Tool) String() string {
	switch t {
	case ToolPen:
		return "pen"
	case ToolEraser:
		return "eraser"
	}
	return ""
}

type Kind uint8

const (
	Enter Kind = iota // tool came into range
	Hover             // moved while in range, not touching
	Down
	Move // moved while touching
	Up
	Leave // tool left range
)

func (k Kind) String() string {
	return [...]string{"enter", "hover", "down", "move", "up", "leave"}[k]
}

// Event is one SYN_REPORT frame. Coordinates are raw digitizer units; use
// Ranges to map them to the screen.
type Event struct {
	Kind         Kind
	Tool         Tool
	X, Y         int32
	Pressure     int32
	Distance     int32
	TiltX, TiltY int32
	Time         int64 // kernel timestamp, µs since the Unix epoch
}

type AbsInfo struct{ Min, Max int32 }

// Norm maps v into [0, 1].
func (a AbsInfo) Norm(v int32) float32 {
	if a.Max == a.Min {
		return 0
	}
	return float32(v-a.Min) / float32(a.Max-a.Min)
}

type Ranges struct{ X, Y, Pressure AbsInfo }

// decoder turns raw input events into Events. One SYN_REPORT frame can carry
// several transitions (a fast pen enters range and touches down in the same
// frame), so it may emit more than one Event per frame, in order.
type decoder struct {
	cur       Event // accumulates ABS values across frames
	tool      Tool
	touch     bool
	prevTool  Tool // as of the last frame
	prevTouch bool
}

func (d *decoder) feed(sec, usec int64, typ, code uint16, value int32, emit func(Event)) {
	switch typ {
	case evKey:
		switch code {
		case btnToolPen:
			d.setTool(ToolPen, value != 0)
		case btnToolRubber:
			d.setTool(ToolEraser, value != 0)
		case btnTouch:
			d.touch = value != 0
		}
	case evAbs:
		switch code {
		case absX:
			d.cur.X = value
		case absY:
			d.cur.Y = value
		case absPressure:
			d.cur.Pressure = value
		case absDistance:
			d.cur.Distance = value
		case absTiltX:
			d.cur.TiltX = value
		case absTiltY:
			d.cur.TiltY = value
		}
	case evSyn:
		if code != synReport {
			return
		}
		ev := d.cur
		ev.Time = sec*1_000_000 + usec
		ev.Tool = d.tool
		if ev.Tool == ToolNone {
			ev.Tool = d.prevTool // Up/Leave report the tool that just left
		}
		changed := false
		send := func(k Kind) { ev.Kind = k; emit(ev); changed = true }
		if d.tool != ToolNone && d.prevTool == ToolNone {
			send(Enter)
		}
		if d.touch && !d.prevTouch {
			send(Down)
		}
		if !d.touch && d.prevTouch {
			send(Up)
		}
		if d.tool == ToolNone && d.prevTool != ToolNone {
			send(Leave)
		}
		if !changed {
			if d.touch {
				send(Move)
			} else if d.tool != ToolNone {
				send(Hover)
			}
		}
		d.prevTool, d.prevTouch = d.tool, d.touch
	}
}

// setTool tracks which end of the pen is in range. Releasing a tool key only
// clears the tool if it is the one currently in range.
func (d *decoder) setTool(t Tool, down bool) {
	if down {
		d.tool = t
	} else if d.tool == t {
		d.tool = ToolNone
	}
}
