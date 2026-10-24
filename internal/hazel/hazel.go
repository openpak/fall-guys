// Package hazel is the server side of the UDP transport the Fall Guys client speaks to its
// round server: Hazel's datagram layer as the client's own Hazel build implements it.
//
// Every datagram starts with a type byte. All of them except Disconnect carry a 16-bit
// big-endian id next:
//
//	0x00 unreliable   [00][seq hi][seq lo] payload
//	0x01 reliable     [01][id hi][id lo]   payload    acknowledged, resent until it is
//	0x08 hello        [08][id hi][id lo][x]           client -> server, reliable
//	0x09 disconnect   [09] (optional payload)
//	0x0a ack          [0a][id hi][id lo][mask]        mask bit i: id-1-i was received too
//	0x0c ping         [0c][id hi][id lo]              reliable, carries nothing
//	0x0d welcome      [0d][id hi][id lo][hazel id u32 LE]  server -> client, reliable
//
// Types 2-7 and 11 are delivered like unreliable ones; 14 is ignored. Reliable delivery is not
// ordered here: the layer above (the HLAPI message processors) sequences its own messages.
//
// Facts come from the client's Hazel.Udp.UdpConnection (DoHandleReceive, SendHello,
// SendWelcome, SendAck, SendPing, UnreliableSend); see docs/protocol.md.
package hazel

import (
	"encoding/binary"
	"errors"
	"log"
	"net"
	"sync"
	"time"
)

const (
	typeUnreliable = 0x00
	typeReliable   = 0x01
	typeHello      = 0x08
	typeDisconnect = 0x09
	typeAck        = 0x0a
	typePing       = 0x0c
	typeWelcome    = 0x0d

	// MaxDatagram is the largest datagram the client sends unfragmented (UnreliableSend
	// counts anything from 1201 bytes as oversized).
	MaxDatagram = 1200

	resendInterval = 150 * time.Millisecond
	// A reliable datagram nobody acknowledges for this long ends the connection.
	ackTimeout = 10 * time.Second
	// A peer silent for this long is gone. The client pings every 1.5 s.
	idleTimeout = 15 * time.Second
)

// Handler receives a connection's events. Calls for one connection never overlap.
type Handler interface {
	// Connected: the client's hello arrived and was welcomed. hello is the hello's payload
	// after the id.
	Connected(c *Conn, hello []byte)
	// Data: a payload, reliable or not.
	Data(c *Conn, payload []byte, reliable bool)
	// Disconnected: the client left or timed out. No more calls follow.
	Disconnected(c *Conn, reason string)
}

// Listener serves one UDP socket.
type Listener struct {
	pc      net.PacketConn
	handler Handler

	mu     sync.Mutex
	conns  map[string]*Conn
	nextID uint32
	closed bool
}

// Listen opens addr ("host:port") and serves it until Close.
func Listen(addr string, h Handler) (*Listener, error) {
	pc, err := net.ListenPacket("udp", addr)
	if err != nil {
		return nil, err
	}
	return Serve(pc, h), nil
}

// Serve runs a listener on an open socket.
func Serve(pc net.PacketConn, h Handler) *Listener {
	l := &Listener{pc: pc, handler: h, conns: map[string]*Conn{}, nextID: 1}
	go l.readLoop()
	go l.tickLoop()
	return l
}

func (l *Listener) Addr() net.Addr { return l.pc.LocalAddr() }

func (l *Listener) Close() error {
	l.mu.Lock()
	l.closed = true
	conns := make([]*Conn, 0, len(l.conns))
	for _, c := range l.conns {
		conns = append(conns, c)
	}
	l.mu.Unlock()
	for _, c := range conns {
		c.Disconnect("server closing")
	}
	return l.pc.Close()
}

func (l *Listener) readLoop() {
	buf := make([]byte, 64*1024)
	for {
		n, from, err := l.pc.ReadFrom(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			continue
		}
		if n == 0 {
			continue
		}
		pkt := append([]byte(nil), buf[:n]...)
		l.receive(pkt, from)
	}
}

func (l *Listener) receive(pkt []byte, from net.Addr) {
	key := from.String()
	l.mu.Lock()
	c := l.conns[key]
	if c == nil {
		if pkt[0] != typeHello || len(pkt) < 3 || l.closed {
			l.mu.Unlock()
			return // nothing but a hello opens a connection
		}
		c = &Conn{l: l, addr: from, ID: l.nextID, pending: map[uint16]*pending{}, lastHeard: time.Now()}
		l.nextID++
		l.conns[key] = c
	}
	l.mu.Unlock()
	c.receive(pkt)
}

func (l *Listener) tickLoop() {
	t := time.NewTicker(50 * time.Millisecond)
	defer t.Stop()
	for range t.C {
		l.mu.Lock()
		if l.closed {
			l.mu.Unlock()
			return
		}
		conns := make([]*Conn, 0, len(l.conns))
		for _, c := range l.conns {
			conns = append(conns, c)
		}
		l.mu.Unlock()
		now := time.Now()
		for _, c := range conns {
			c.tick(now)
		}
	}
}

func (l *Listener) forget(c *Conn) {
	l.mu.Lock()
	if l.conns[c.addr.String()] == c {
		delete(l.conns, c.addr.String())
	}
	l.mu.Unlock()
}

type pending struct {
	data     []byte
	first    time.Time
	lastSent time.Time
}

// Conn is one client.
type Conn struct {
	l    *Listener
	addr net.Addr
	// ID is the hazel id the welcome gave the client.
	ID uint32

	mu        sync.Mutex
	welcomed  bool
	gone      bool
	sendID    uint16 // last reliable id used
	sendSeq   uint16 // last unreliable sequence used
	pending   map[uint16]*pending
	recv      [65536 / 64]uint64 // reliable ids seen (bitset), cleared half a window ahead
	recvLast  uint16
	recvAny   bool
	lastHeard time.Time
	rtt       time.Duration

	// events serialises handler calls for this connection.
	events sync.Mutex
}

func (c *Conn) Addr() net.Addr { return c.addr }

// RTT is the last measured round trip of a reliable datagram.
func (c *Conn) RTT() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.rtt
}

func (c *Conn) receive(pkt []byte) {
	c.mu.Lock()
	if c.gone {
		c.mu.Unlock()
		return
	}
	c.lastHeard = time.Now()
	c.mu.Unlock()

	switch t := pkt[0]; t {
	case typeDisconnect:
		c.close("client disconnected", false)
	case typeAck:
		if len(pkt) >= 3 {
			id := binary.BigEndian.Uint16(pkt[1:])
			var mask byte
			if len(pkt) >= 4 {
				mask = pkt[3]
			}
			c.acked(id, mask)
		}
	case typeHello, typeReliable, typePing:
		if len(pkt) < 3 {
			return
		}
		id := binary.BigEndian.Uint16(pkt[1:])
		isNew := c.ackReceived(id)
		if !isNew {
			return
		}
		switch t {
		case typeHello:
			c.mu.Lock()
			first := !c.welcomed
			c.welcomed = true
			c.mu.Unlock()
			if first {
				c.sendWelcome()
				c.emit(func(h Handler) { h.Connected(c, pkt[3:]) })
			}
		case typeReliable:
			c.emit(func(h Handler) { h.Data(c, pkt[3:], true) })
		}
	case 14:
	default: // 0, 2-7, 11: unreliable framing
		if len(pkt) < 3 {
			return
		}
		c.emit(func(h Handler) { h.Data(c, pkt[3:], false) })
	}
}

func (c *Conn) emit(f func(Handler)) {
	c.events.Lock()
	defer c.events.Unlock()
	c.mu.Lock()
	gone := c.gone
	c.mu.Unlock()
	if !gone {
		f(c.l.handler)
	}
}

// ackReceived acknowledges reliable id and reports whether it is new.
func (c *Conn) ackReceived(id uint16) bool {
	c.mu.Lock()
	isNew := !c.seen(id)
	if isNew {
		c.mark(id)
	}
	var mask byte
	for i := 0; i < 8; i++ {
		if c.seen(id - 1 - uint16(i)) {
			mask |= 1 << i
		}
	}
	c.mu.Unlock()
	c.write([]byte{typeAck, byte(id >> 8), byte(id), mask})
	return isNew
}

func (c *Conn) seen(id uint16) bool { return c.recv[id/64]&(1<<(id%64)) != 0 }

func (c *Conn) mark(id uint16) {
	// Moving forward: forget the ids half the window ahead, so a wrapped id is new again.
	if !c.recvAny || int16(id-c.recvLast) > 0 {
		for x := c.recvLast + 0x8000; c.recvAny && x != id+0x8000; x++ {
			c.recv[x/64] &^= 1 << (x % 64)
		}
		c.recvLast, c.recvAny = id, true
	}
	c.recv[id/64] |= 1 << (id % 64)
}

func (c *Conn) acked(id uint16, mask byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if p, ok := c.pending[id]; ok {
		c.rtt = time.Since(p.lastSent)
		delete(c.pending, id)
	}
	for i := 0; i < 8; i++ {
		if mask&(1<<i) != 0 {
			delete(c.pending, id-1-uint16(i))
		}
	}
}

func (c *Conn) sendWelcome() {
	b := make([]byte, 7)
	b[0] = typeWelcome
	binary.LittleEndian.PutUint32(b[3:], c.ID)
	c.sendTracked(b)
}

// SendReliable queues payload as one reliable datagram.
func (c *Conn) SendReliable(payload []byte) {
	b := make([]byte, 3+len(payload))
	b[0] = typeReliable
	copy(b[3:], payload)
	c.sendTracked(b)
}

// SendUnreliable sends payload once.
func (c *Conn) SendUnreliable(payload []byte) {
	b := make([]byte, 3+len(payload))
	b[0] = typeUnreliable
	copy(b[3:], payload)
	c.mu.Lock()
	if c.gone {
		c.mu.Unlock()
		return
	}
	c.sendSeq++
	binary.BigEndian.PutUint16(b[1:], c.sendSeq)
	c.mu.Unlock()
	c.write(b)
}

func (c *Conn) sendTracked(b []byte) {
	c.mu.Lock()
	if c.gone {
		c.mu.Unlock()
		return
	}
	c.sendID++
	binary.BigEndian.PutUint16(b[1:], c.sendID)
	now := time.Now()
	c.pending[c.sendID] = &pending{data: b, first: now, lastSent: now}
	c.mu.Unlock()
	c.write(b)
}

func (c *Conn) write(b []byte) {
	if len(b) > MaxDatagram {
		log.Printf("[hazel] %s: datagram of %d bytes is over the client's limit", c.addr, len(b))
	}
	_, _ = c.l.pc.WriteTo(b, c.addr)
}

func (c *Conn) tick(now time.Time) {
	c.mu.Lock()
	if c.gone {
		c.mu.Unlock()
		return
	}
	if now.Sub(c.lastHeard) > idleTimeout {
		c.mu.Unlock()
		c.close("timed out", true)
		return
	}
	interval := resendInterval
	if r := 2 * c.rtt; r > interval {
		interval = min(r, time.Second)
	}
	var resend [][]byte
	for _, p := range c.pending {
		if now.Sub(p.first) > ackTimeout {
			c.mu.Unlock()
			c.close("reliable data not acknowledged", true)
			return
		}
		if now.Sub(p.lastSent) >= interval {
			p.lastSent = now
			resend = append(resend, p.data)
		}
	}
	c.mu.Unlock()
	for _, b := range resend {
		c.write(b)
	}
}

// Disconnect tells the client to go and drops the connection.
func (c *Conn) Disconnect(reason string) { c.close(reason, true) }

func (c *Conn) close(reason string, notify bool) {
	c.mu.Lock()
	if c.gone {
		c.mu.Unlock()
		return
	}
	c.gone = true
	c.mu.Unlock()
	if notify {
		c.write([]byte{typeDisconnect})
	}
	c.l.forget(c)
	// After any event in flight: a handler may be the one closing the connection.
	go func() {
		c.events.Lock()
		defer c.events.Unlock()
		c.l.handler.Disconnected(c, reason)
	}()
}
