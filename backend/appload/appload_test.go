package appload

import (
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestParts(t *testing.T) {
	if Parts(1, strings.Repeat("x", MaxPart)) != nil {
		t.Fatal("a body of MaxPart bytes must go as one packet")
	}
	// Multi-byte characters straddle every cut point.
	body := strings.Repeat("ab…é", 3*MaxPart/7)
	parts := Parts(1, body)
	if len(parts) < 2 {
		t.Fatalf("%d parts for %d bytes", len(parts), len(body))
	}
	var joined strings.Builder
	for i, p := range parts {
		if p.Type != MsgPart || len(p.Body) > MaxPart {
			t.Fatalf("part %d: type %d, %d bytes", i, p.Type, len(p.Body))
		}
		head, slice, _ := strings.Cut(p.Body, "\n")
		if want := fmt.Sprintf("1 %d %d", i, len(parts)); head != want {
			t.Fatalf("part %d header %q, want %q", i, head, want)
		}
		if !utf8.ValidString(slice) {
			t.Fatalf("part %d cuts a character", i)
		}
		joined.WriteString(slice)
	}
	if joined.String() != body {
		t.Fatal("parts don't join back to the body")
	}
}

func TestAwaitClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s")
	l, err := net.ListenUnix("unixpacket", &net.UnixAddr{Name: path, Net: "unixpacket"})
	if err != nil {
		t.Skipf("no unixpacket sockets here (they are on the tablet): %v", err)
	}
	defer l.Close()
	go func() {
		peer, err := l.AcceptUnix()
		if err != nil {
			return
		}
		peer.Write([]byte("late packet"))
		time.Sleep(100 * time.Millisecond)
		peer.Close()
	}()
	c, err := Dial(path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	start := time.Now()
	c.AwaitClose(5 * time.Second)
	if d := time.Since(start); d < 90*time.Millisecond || d > 2*time.Second {
		t.Fatalf("returned after %v, want just after the peer closed", d)
	}
}
