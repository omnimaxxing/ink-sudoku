// Command probe is the backend of the G1 probe app. It forwards every raw pen
// event (with its kernel timestamp) to the QML frontend, so the frontend can
// compare that path against the mouse events xochitl delivers, and it appends
// the frontend's measurements to a file we can scp off the tablet.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync/atomic"
	"time"

	"sudoku/appload"
	"sudoku/pen"
)

// Message types; keep in sync with probe/ui/main.qml.
const (
	msgInfo    = 1  // backend -> ui: device info
	msgPen     = 2  // backend -> ui: one pen event
	msgStats   = 3  // backend -> ui: per-second stats
	msgResults = 10 // ui -> backend: measurements to save
	msgSaved   = 11 // backend -> ui: save confirmation
)

const resultsPath = "/home/root/sudoku-probe.jsonl"

type info struct {
	OK     bool       `json:"ok"`
	Error  string     `json:"error,omitempty"`
	Name   string     `json:"name"`
	Path   string     `json:"path"`
	Ranges pen.Ranges `json:"ranges"`
}

type penMsg struct {
	K  string `json:"k"`
	T  string `json:"t"`
	X  int32  `json:"x"`
	Y  int32  `json:"y"`
	P  int32  `json:"p"`
	D  int32  `json:"d"`
	TX int32  `json:"tx"`
	TY int32  `json:"ty"`
	TS int64  `json:"ts"` // µs, kernel timestamp (evdev default clock is CLOCK_REALTIME)
	RX int64  `json:"rx"` // µs, when this backend read it; separates kernel→backend from backend→QML
}

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: probe <appload socket>")
	}
	conn, err := appload.Dial(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	dev, err := pen.Open()
	in := info{OK: err == nil}
	if err != nil {
		in.Error = err.Error()
	} else {
		in.Name, in.Path, in.Ranges = dev.Name, dev.Path, dev.Ranges
	}
	send(conn, msgInfo, in)

	if dev != nil {
		var count, maxP atomic.Int64
		go func() {
			err := dev.Run(func(e pen.Event) {
				count.Add(1)
				if int64(e.Pressure) > maxP.Load() {
					maxP.Store(int64(e.Pressure))
				}
				send(conn, msgPen, penMsg{
					K: e.Kind.String(), T: e.Tool.String(),
					X: e.X, Y: e.Y, P: e.Pressure, D: e.Distance,
					TX: e.TiltX, TY: e.TiltY, TS: e.Time, RX: time.Now().UnixMicro(),
				})
			})
			log.Printf("pen reader stopped: %v", err)
		}()
		go func() {
			for range time.Tick(time.Second) {
				send(conn, msgStats, map[string]int64{"rate": count.Swap(0), "maxPressure": maxP.Load()})
			}
		}()
	}

	for {
		m, err := conn.Recv()
		if err != nil || m.Type == appload.MsgTerminate {
			return
		}
		if m.Type == msgResults {
			err := appendLine(resultsPath, m.Body)
			status := "saved to " + resultsPath
			if err != nil {
				status = "save failed: " + err.Error()
			}
			send(conn, msgSaved, map[string]string{"status": status})
		}
	}
}

func send(conn *appload.Conn, typ int32, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		log.Printf("marshal: %v", err)
		return
	}
	if err := conn.Send(typ, string(b)); err != nil {
		log.Printf("send: %v", err)
	}
}

func appendLine(path, line string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintln(f, line)
	return err
}
