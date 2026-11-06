package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"sudoku/appload"
	"sudoku/game"
)

// TestRestoreParts restores a real saved game ($INK_RESTORE/state.json, e.g.
// one copied off the tablet), sends hello, and writes what the backend puts
// on the socket for it (big messages as appload.Parts) to $INK_PARTS_OUT as
// JSON [[type, body], …], for the Mac stub's --parts mode (dev/shot.qml).
// From backend/:
//
//	INK_RESTORE=<dir> INK_PARTS_OUT=/tmp/parts.json pixi run go test ./cmd/sudoku -run RestoreParts
func TestRestoreParts(t *testing.T) {
	src, out := os.Getenv("INK_RESTORE"), os.Getenv("INK_PARTS_OUT")
	if src == "" || out == "" {
		t.Skip("set INK_RESTORE=<dir with state.json> and INK_PARTS_OUT=<file>")
	}
	b, err := os.ReadFile(filepath.Join(src, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "state.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	var wire [][2]any
	s := newServer(game.Store{Dir: dir}, func(typ int32, body string) {
		if parts := appload.Parts(typ, body); parts != nil {
			t.Logf("type %d: %d bytes as %d parts", typ, len(body), len(parts))
			for _, p := range parts {
				wire = append(wire, [2]any{p.Type, p.Body})
			}
			return
		}
		wire = append(wire, [2]any{typ, body})
	})
	s.handle(msgHello, `{"gridSize": 1180}`)
	j, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, j, 0o644); err != nil {
		t.Fatal(err)
	}
}
