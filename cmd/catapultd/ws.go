package main

// A minimal RFC 6455 server side: the upgrade, then binary messages in both directions.
// The gateway client (System.Net.WebSockets.ClientWebSocket) sends masked binary frames and
// pings; nothing here needs extensions or sub-protocols.

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
)

const wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

type wsConn struct {
	c  net.Conn
	r  *bufio.Reader
	mu sync.Mutex // writes
}

func wsUpgrade(w http.ResponseWriter, r *http.Request) (*wsConn, error) {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return nil, errors.New("not a websocket upgrade")
	}
	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		return nil, errors.New("no Sec-WebSocket-Key")
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		return nil, errors.New("connection cannot be hijacked")
	}
	c, rw, err := hj.Hijack()
	if err != nil {
		return nil, err
	}
	sum := sha1.Sum([]byte(key + wsGUID))
	resp := "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + base64.StdEncoding.EncodeToString(sum[:]) + "\r\n\r\n"
	if _, err := c.Write([]byte(resp)); err != nil {
		c.Close()
		return nil, err
	}
	return &wsConn{c: c, r: rw.Reader}, nil
}

// read returns the next complete data message, answering pings and handling close.
func (ws *wsConn) read() ([]byte, error) {
	var msg []byte
	for {
		var h [2]byte
		if _, err := io.ReadFull(ws.r, h[:]); err != nil {
			return nil, err
		}
		fin, op := h[0]&0x80 != 0, h[0]&0x0f
		masked, n := h[1]&0x80 != 0, uint64(h[1]&0x7f)
		switch n {
		case 126:
			var b [2]byte
			if _, err := io.ReadFull(ws.r, b[:]); err != nil {
				return nil, err
			}
			n = uint64(binary.BigEndian.Uint16(b[:]))
		case 127:
			var b [8]byte
			if _, err := io.ReadFull(ws.r, b[:]); err != nil {
				return nil, err
			}
			n = binary.BigEndian.Uint64(b[:])
		}
		if n > 16<<20 {
			return nil, errors.New("websocket frame too large")
		}
		var mask [4]byte
		if masked {
			if _, err := io.ReadFull(ws.r, mask[:]); err != nil {
				return nil, err
			}
		}
		p := make([]byte, n)
		if _, err := io.ReadFull(ws.r, p); err != nil {
			return nil, err
		}
		if masked {
			for i := range p {
				p[i] ^= mask[i%4]
			}
		}
		switch op {
		case 0x8: // close
			_ = ws.writeFrame(0x8, p)
			return nil, io.EOF
		case 0x9: // ping
			if err := ws.writeFrame(0xA, p); err != nil {
				return nil, err
			}
			continue
		case 0xA: // pong
			continue
		}
		msg = append(msg, p...)
		if fin {
			return msg, nil
		}
	}
}

func (ws *wsConn) write(p []byte) error { return ws.writeFrame(0x2, p) }

func (ws *wsConn) writeFrame(op byte, p []byte) error {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	return ws.writeFrameLocked(op, p)
}

// writeFrameLocked is writeFrame for a caller that already holds ws.mu.
func (ws *wsConn) writeFrameLocked(op byte, p []byte) error {
	h := []byte{0x80 | op}
	switch {
	case len(p) < 126:
		h = append(h, byte(len(p)))
	case len(p) < 1<<16:
		h = append(h, 126, byte(len(p)>>8), byte(len(p)))
	default:
		h = append(h, 127)
		h = binary.BigEndian.AppendUint64(h, uint64(len(p)))
	}
	_, err := ws.c.Write(append(h, p...))
	return err
}

func (ws *wsConn) close() { ws.c.Close() }
