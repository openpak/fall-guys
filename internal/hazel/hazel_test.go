package hazel

import (
	"bytes"
	"encoding/binary"
	"net"
	"sync"
	"testing"
	"time"
)

type recorder struct {
	mu     sync.Mutex
	conns  []*Conn
	hellos [][]byte
	data   []string
	gone   []string
	echo   bool
}

func (r *recorder) Connected(c *Conn, hello []byte) {
	r.mu.Lock()
	r.conns = append(r.conns, c)
	r.hellos = append(r.hellos, hello)
	r.mu.Unlock()
}

func (r *recorder) Data(c *Conn, p []byte, reliable bool) {
	r.mu.Lock()
	r.data = append(r.data, string(p))
	echo := r.echo
	r.mu.Unlock()
	if echo {
		c.SendReliable(append([]byte("echo:"), p...))
	}
}

func (r *recorder) Disconnected(c *Conn, reason string) {
	r.mu.Lock()
	r.gone = append(r.gone, reason)
	r.mu.Unlock()
}

func (r *recorder) snapshot() ([]string, []string, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.data...), append([]string(nil), r.gone...), len(r.conns)
}

type client struct {
	t  *testing.T
	pc net.PacketConn
	to net.Addr
	id uint16
}

func dial(t *testing.T, l *Listener) *client {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pc.Close() })
	return &client{t: t, pc: pc, to: l.Addr()}
}

func (c *client) send(b ...byte) { _, _ = c.pc.WriteTo(b, c.to) }

func (c *client) reliable(typ byte, payload string) uint16 {
	c.id++
	b := []byte{typ, byte(c.id >> 8), byte(c.id)}
	c.send(append(b, payload...)...)
	return c.id
}

func (c *client) read() []byte {
	c.t.Helper()
	buf := make([]byte, 2048)
	_ = c.pc.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, err := c.pc.ReadFrom(buf)
	if err != nil {
		c.t.Fatalf("read: %v", err)
	}
	return buf[:n]
}

// readType skips datagrams until one of type typ arrives.
func (c *client) readType(typ byte) []byte {
	c.t.Helper()
	for {
		if b := c.read(); b[0] == typ {
			return b
		}
	}
}

func eventually(t *testing.T, f func() bool) {
	t.Helper()
	for end := time.Now().Add(2 * time.Second); time.Now().Before(end); time.Sleep(5 * time.Millisecond) {
		if f() {
			return
		}
	}
	t.Fatal("condition not met")
}

func TestHelloWelcomeAndReliableData(t *testing.T) {
	r := &recorder{echo: true}
	l, err := Listen("127.0.0.1:0", r)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	c := dial(t, l)

	c.reliable(typeHello, "\x00")
	if ack := c.read(); !bytes.Equal(ack, []byte{typeAck, 0, 1, 0}) {
		t.Fatalf("hello ack %x", ack)
	}
	w := c.read()
	if len(w) != 7 || w[0] != typeWelcome || binary.LittleEndian.Uint32(w[3:]) == 0 {
		t.Fatalf("welcome %x", w)
	}
	c.send(typeAck, w[1], w[2], 0)

	id := c.reliable(typeReliable, "hi")
	ack := c.readType(typeAck)
	if binary.BigEndian.Uint16(ack[1:]) != id || ack[3] != 1 { // the hello (id-1) was seen too
		t.Fatalf("data ack %x", ack)
	}
	echo := c.readType(typeReliable)
	if string(echo[3:]) != "echo:hi" {
		t.Fatalf("echo %q", echo[3:])
	}
	// A duplicate is acknowledged again but not delivered twice.
	c.send(typeReliable, byte(id>>8), byte(id), 'h', 'i')
	c.readType(typeAck)
	c.send(typeUnreliable, 0, 1, 'u')
	eventually(t, func() bool { d, _, _ := r.snapshot(); return len(d) == 2 })
	if d, _, n := r.snapshot(); d[0] != "hi" || d[1] != "u" || n != 1 {
		t.Fatalf("data %q conns %d", d, n)
	}

	// Unacknowledged reliable data is sent again.
	for again := c.readType(typeReliable); !bytes.Equal(again, echo); again = c.readType(typeReliable) {
	}
	c.send(typeAck, echo[1], echo[2], 0)

	c.send(typeDisconnect)
	eventually(t, func() bool { _, g, _ := r.snapshot(); return len(g) == 1 })
}

func TestOnlyHelloOpensAConnection(t *testing.T) {
	r := &recorder{}
	l, _ := Listen("127.0.0.1:0", r)
	defer l.Close()
	c := dial(t, l)
	c.reliable(typeReliable, "x")
	c.reliable(typePing, "")
	time.Sleep(100 * time.Millisecond)
	if d, _, n := r.snapshot(); n != 0 || len(d) != 0 {
		t.Fatalf("connection opened without hello: %d %q", n, d)
	}
}

func TestReceiveWindowWraps(t *testing.T) {
	c := &Conn{}
	for i := 0; i < 3*65536; i++ {
		id := uint16(i)
		if c.seen(id) {
			t.Fatalf("id %d seen before it was sent (round %d)", id, i/65536)
		}
		c.mark(id)
	}
}
