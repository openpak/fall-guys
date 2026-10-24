package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"openpak/fall-guys/internal/gamemsg"
	"openpak/fall-guys/internal/hazel"
	"openpak/fall-guys/internal/hlapi"
	"openpak/fall-guys/internal/round"
)

// A client over real UDP gets through hello, the key exchange and the entry check.
func TestConnectOverUDP(t *testing.T) {
	secret := []byte("s3cret")
	cat := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer s3cret" {
			w.WriteHeader(401)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"matchId": "m", "players": []string{r.URL.Query().Get("account"), "other"}})
	}))
	defer cat.Close()
	d := &directory{secret: secret, catapult: cat.URL, client: cat.Client()}
	s := round.New(round.Config{Directory: d, Level: round.Levels[round.DefaultLevel]})
	l, err := hazel.Listen("127.0.0.1:0", adapter{s})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	c, _ := net.Dial("udp", l.Addr().String())
	defer c.Close()
	id := uint16(1)
	send := func(payload []byte) {
		id++
		_, _ = c.Write(append([]byte{1, byte(id >> 8), byte(id)}, payload...))
	}
	// next returns the next game message the server sends, acking reliable ones.
	next := func() *hlapi.Reader {
		buf := make([]byte, 2048)
		for {
			_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
			n, err := c.Read(buf)
			if err != nil {
				t.Fatal(err)
			}
			if buf[0] == 1 {
				_, _ = c.Write([]byte{10, buf[1], buf[2], 0})
				return hlapi.NewReader(append([]byte(nil), buf[3:n]...))
			}
		}
	}
	_, _ = c.Write([]byte{8, 0, 1, 0})

	var pv [8]byte
	binary.LittleEndian.PutUint64(pv[:], gamemsg.ProtocolVersion)
	w := hlapi.Writer{}
	w.Start(gamemsg.ClientConnectInitial.MsgID())
	w.I32(0x03E6EB23)
	w.I32(8 + 3)
	for _, b := range pv {
		w.I32(int32(b))
	}
	w.String("")
	w.String("")
	w.I32(0x03E6EB23)
	send(w.B)

	r := next()
	if r.Byte() != gamemsg.ServerConnectInitial.MsgID() {
		t.Fatal("no connect initial")
	}
	for k := r.I32(); k > 0; k-- {
		r.F32()
	}
	key := r.U64()

	m := hmac.New(sha256.New, secret)
	m.Write([]byte("entry\x00acct-1"))
	tok := "acct-1." + base64.RawURLEncoding.EncodeToString(m.Sum(nil))
	w = hlapi.Writer{}
	w.Start(gamemsg.ClientConnectClient.MsgID())
	w.U64(gamemsg.ConnectProof(key))
	w.U64(42)
	w.Byte(0)
	gamemsg.ShortString(&w, tok)
	w.Bool(false)
	w.Byte(0)
	w.Byte(1)
	gamemsg.ShortString(&w, "")
	w.Bool(false)
	w.Bool(false)
	gamemsg.ShortString(&w, "")
	gamemsg.ShortString(&w, "")
	send(w.B)

	r = next()
	if r.Byte() != gamemsg.ServerConnectedClient.MsgID() || r.I32() != gamemsg.ConnectParticipant {
		t.Fatal("not admitted")
	}
}
