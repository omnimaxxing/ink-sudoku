// Package display follows whether the tablet's display is awake, from
// xochitl's log: "Changing display state from Normal to DeepSleep" when the
// player sleeps the tablet (power button, folio, idle timer), "… to Normal"
// when it wakes. LightSleep and DeepSleep both mean asleep. The kernel also
// suspends between pen strokes with the page on screen; that isn't sleep and
// isn't logged this way.
package display

import (
	"regexp"
)

var change = regexp.MustCompile(`Changing display state from \w+ to (\w+)`)

// awake reports the state a log line changes the display to, if it is one.
func awake(line string) (awake, ok bool) {
	m := change.FindStringSubmatch(line)
	if m == nil {
		return false, false
	}
	return m[1] == "Normal", true
}
