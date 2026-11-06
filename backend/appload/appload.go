// Package appload speaks AppLoad's backend protocol. AppLoad starts the
// backend with argv[1] set to a SOCK_SEQPACKET unix socket. Every message is
// an 8-byte header packet {int32 type, uint32 length} (native byte order),
// followed by the payload as a packet of its own when length > 0.
//
// The kernel refuses a packet bigger than the socket's send buffer (208 KiB
// by default on the tablet) with EMSGSIZE, so Send splits bigger messages
// into MsgPart messages that the frontend joins (see Parts).
package appload

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
	"unicode/utf8"
)

// System message types sent by AppLoad itself.
const (
	MsgTerminate       int32 = -1
	MsgNewCoordinator  int32 = -2
	MsgLostCoordinator int32 = -3
)

// MsgPart carries one piece of a message too big for one packet. Its body is
// "<type> <index> <count>\n" followed by the next slice of the original body;
// joining the slices of parts 0..count-1 gives the original message.
const MsgPart int32 = 50

// MaxPart is the largest body sent as one packet, far under the send buffer.
const MaxPart = 64 << 10

const maxMessage = 10 << 20

type Message struct {
	Type int32
	Body string
}

type Conn struct {
	c  *net.UnixConn
	mu sync.Mutex // header and payload packets must go out back to back
}

func Dial(path string) (*Conn, error) {
	c, err := net.DialUnix("unixpacket", nil, &net.UnixAddr{Name: path, Net: "unixpacket"})
	if err != nil {
		return nil, err
	}
	return &Conn{c: c}, nil
}

// Send sends one message, as MsgPart pieces if it is bigger than MaxPart.
func (c *Conn) Send(typ int32, body string) error {
	if len(body) > maxMessage {
		return fmt.Errorf("appload: message of %d bytes exceeds %d", len(body), maxMessage)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	parts := Parts(typ, body)
	if parts == nil {
		return c.send(typ, body)
	}
	for _, p := range parts {
		if err := c.send(p.Type, p.Body); err != nil {
			return err
		}
	}
	return nil
}

func (c *Conn) send(typ int32, body string) error {
	var h [8]byte
	binary.LittleEndian.PutUint32(h[0:], uint32(typ))
	binary.LittleEndian.PutUint32(h[4:], uint32(len(body)))
	if _, err := c.c.Write(h[:]); err != nil {
		return err
	}
	if len(body) == 0 {
		return nil
	}
	_, err := c.c.Write([]byte(body))
	return err
}

// Parts splits a body bigger than MaxPart into MsgPart messages, cutting only
// between UTF-8 characters (the frontend decodes each part on its own). It
// returns nil when the body fits in one packet.
func Parts(typ int32, body string) []Message {
	if len(body) <= MaxPart {
		return nil
	}
	const room = MaxPart - 32 // for the "<type> <index> <count>\n" line
	var slices []string
	for rest := body; rest != ""; {
		n := min(room, len(rest))
		for n < len(rest) && !utf8.RuneStart(rest[n]) {
			n--
		}
		slices = append(slices, rest[:n])
		rest = rest[n:]
	}
	parts := make([]Message, len(slices))
	for i, s := range slices {
		parts[i] = Message{Type: MsgPart, Body: fmt.Sprintf("%d %d %d\n", typ, i, len(slices)) + s}
	}
	return parts
}

// Recv blocks for the next message; it fails once the socket closes.
func (c *Conn) Recv() (Message, error) {
	var h [8]byte
	n, err := c.c.Read(h[:])
	if err != nil {
		return Message{}, err
	}
	if n != len(h) {
		return Message{}, fmt.Errorf("appload: short header (%d bytes)", n)
	}
	typ := int32(binary.LittleEndian.Uint32(h[0:]))
	size := binary.LittleEndian.Uint32(h[4:])
	if size > maxMessage {
		return Message{}, errors.New("appload: message exceeds protocol limit")
	}
	if size == 0 {
		return Message{Type: typ}, nil
	}
	buf := make([]byte, size)
	n, err = c.c.Read(buf)
	if err != nil {
		return Message{}, err
	}
	return Message{Type: typ, Body: string(buf[:n])}, nil
}

func (c *Conn) Close() error { return c.c.Close() }

// AwaitClose discards anything still arriving until the other end closes
// the socket, or until timeout passes.
func (c *Conn) AwaitClose(timeout time.Duration) {
	c.c.SetReadDeadline(time.Now().Add(timeout))
	buf := make([]byte, 64<<10)
	for {
		if _, err := c.c.Read(buf); err != nil {
			return
		}
	}
}
