package display

import (
	"bufio"
	"os/exec"
	"runtime"
	"syscall"
)

// Watch calls onChange with every display change until the log stream ends.
// The journalctl it follows dies with the calling thread (Pdeathsig), which
// Watch keeps for itself, so it never outlives the backend.
func Watch(onChange func(awake bool)) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	cmd := exec.Command("journalctl", "-u", "xochitl", "-f", "-n", "0", "-o", "cat")
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		if a, ok := awake(sc.Text()); ok {
			onChange(a)
		}
	}
	return cmd.Wait()
}
