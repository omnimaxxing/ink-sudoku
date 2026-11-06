package display

import "testing"

// Lines as xochitl 3.29 logs them (journalctl -o cat).
func TestAwake(t *testing.T) {
	for line, want := range map[string][2]bool{
		"03:56:57.440 rm.batterymanager        Changing display state from Normal to DeepSleep (setDisplayState /home/runner/work/xochitl/x": {false, true},
		"04:00:22.700 rm.batterymanager        Changing display state from DeepSleep to Normal (setDisplayState /home/runner/work/xochitl/x": {true, true},
		"rm.batterymanager        Changing display state from Normal to LightSleep (setDisplayState":                                         {false, true},
		"17:33:02.986 rm.batterymanager        Entering LightSleep for 9587978ms, battery is 100 (goToSleep":                                 {false, false},
	} {
		if a, ok := awake(line); a != want[0] || ok != want[1] {
			t.Errorf("%q: awake %v ok %v, want %v", line, a, ok, want)
		}
	}
}
