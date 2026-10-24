package main

import (
	"bufio"
	"encoding/binary"
	"io"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"openpak/fall-guys/internal/catapult"
)

// wsDial is a test client: upgrade, then masked binary frames out, unmasked frames in.
func wsDial(t *testing.T, addr string) (net.Conn, *bufio.Reader) {
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(c, "GET /ws HTTP/1.1\r\nHost: x\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n"+
		"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\n\r\n")
	r := bufio.NewReader(c)
	status, _ := r.ReadString('\n')
	if !strings.Contains(status, "101") {
		t.Fatalf("upgrade: %q", status)
	}
	for {
		l, _ := r.ReadString('\n')
		if l == "\r\n" {
			break
		}
	}
	return c, r
}

func wsSend(c net.Conn, p []byte) {
	h := []byte{0x82}
	if len(p) < 126 {
		h = append(h, 0x80|byte(len(p)))
	} else {
		h = append(h, 0x80|126, byte(len(p)>>8), byte(len(p)))
	}
	mask := []byte{1, 2, 3, 4}
	out := append(h, mask...)
	for i, b := range p {
		out = append(out, b^mask[i%4])
	}
	_, _ = c.Write(out)
}

func wsRecv(t *testing.T, r *bufio.Reader) []byte {
	var h [2]byte
	if _, err := io.ReadFull(r, h[:]); err != nil {
		t.Fatal(err)
	}
	n := int(h[1] & 0x7f)
	if n == 126 {
		var b [2]byte
		_, _ = io.ReadFull(r, b[:])
		n = int(binary.BigEndian.Uint16(b[:]))
	}
	p := make([]byte, n)
	_, _ = io.ReadFull(r, p)
	return p
}

func TestGatewayAuthentication(t *testing.T) {
	s, _ := testServer(t)
	var err error
	if s.schema, err = catapult.LoadSchema(); err != nil {
		t.Fatal(err)
	}
	if s.replies, err = catapult.Replies(); err != nil {
		t.Fatal(err)
	}
	s.handlers = gatewayHandlers()
	srv := httptest.NewServer(s)
	defer srv.Close()
	c, r := wsDial(t, strings.TrimPrefix(srv.URL, "http://"))
	defer c.Close()

	tok := s.sessionToken("acc-1", time.Now().Add(time.Hour))
	req := map[string]any{
		"$type":   catapult.MessageOf(gw + "AuthenticationRequest"),
		"Headers": []any{map[string]any{"$type": catapult.OperationHeader, "RequestId": "0f8fad5b-d9cb-469f-a165-70867728950e", "Type": int64(catapult.OpRequest)}},
		"Body":    map[string]any{"Token": tok, "ProtocolVersion": "1"},
	}
	p, err := s.schema.Encode(catapult.MessageType, req)
	if err != nil {
		t.Fatal(err)
	}
	wsSend(c, p)
	reply, err := s.schema.Decode(catapult.MessageType, wsRecv(t, r))
	if err != nil {
		t.Fatal(err)
	}
	if reply["$type"] != catapult.ResultOf(gw+"AuthenticationResponse") {
		t.Fatalf("reply type %v", reply["$type"])
	}
	if b := reply["Body"].(map[string]any); b["UserId"] != "acc-1" {
		t.Fatalf("body %v", b)
	}
	id, op := operation(reply)
	if id != "0f8fad5b-d9cb-469f-a165-70867728950e" || op != catapult.OpReply {
		t.Fatalf("operation %s %d", id, op)
	}
}
