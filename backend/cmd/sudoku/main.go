// Command sudoku is the Ink Sudoku backend that AppLoad starts next to the
// QML frontend. It owns the game (see PROTOCOL.md) and talks to the frontend
// over the AppLoad socket passed as argv[1]. It logs to backend.log in the
// data dir ($INK_SUDOKU_DIR, default /home/root/.local/share/ink-sudoku).
package main

import (
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"sudoku/appload"
	"sudoku/display"
	"sudoku/game"
)

const maxLog = 1 << 20

func main() {
	dir := game.DefaultDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("data dir: %v", err)
	}
	if f, err := openLog(filepath.Join(dir, "backend.log")); err == nil {
		log.SetOutput(f)
		defer f.Close()
	}
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	if len(os.Args) < 2 {
		log.Fatal("usage: sudoku <appload socket>")
	}
	conn, err := appload.Dial(os.Args[1])
	if err != nil {
		log.Fatalf("dial %s: %v", os.Args[1], err)
	}
	log.Printf("started, pid %d", os.Getpid())

	s := newServer(game.Store{Dir: dir, Shared: []string{game.KakuroSamples()}}, func(typ int32, body string) {
		if err := conn.Send(typ, body); err != nil {
			log.Printf("send %d: %v", typ, err)
		}
	})

	// The timer only runs while the display is awake.
	go func() { log.Printf("display watch: %v", display.Watch(s.display)) }()

	// A signal closes the socket, which ends the loop below cleanly.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	go func() {
		log.Printf("signal %v", <-sigs)
		conn.Close()
	}()

	for {
		m, err := conn.Recv()
		if err != nil {
			log.Printf("socket closed: %v", err)
			break
		}
		if m.Type == appload.MsgTerminate {
			// Wait for AppLoad to close its end first; see piano's main.
			log.Print("terminate")
			conn.AwaitClose(2 * time.Second)
			break
		}
		s.handle(m.Type, m.Body)
	}
	s.close()
	conn.Close()
	log.Print("exit")
}

// openLog appends to path, first moving it to path.1 once it passes maxLog.
func openLog(path string) (*os.File, error) {
	if fi, err := os.Stat(path); err == nil && fi.Size() > maxLog {
		os.Rename(path, path+".1")
	}
	return os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
}
