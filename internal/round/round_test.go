package round

import (
	"encoding/binary"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"openpak/fall-guys/internal/gamemsg"
	"openpak/fall-guys/internal/hlapi"
)

type fakeConn struct {
	mu   sync.Mutex
	msgs [][]byte
	gone bool
}

func (c *fakeConn) SendReliable(b []byte)   { c.mu.Lock(); c.msgs = append(c.msgs, b); c.mu.Unlock() }
func (c *fakeConn) SendUnreliable(b []byte) { c.SendReliable(b) }
func (c *fakeConn) Disconnect(string)       { c.mu.Lock(); c.gone = true; c.mu.Unlock() }

// types lists the game message types received so far.
func (c *fakeConn) types() []gamemsg.Type {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []gamemsg.Type
	for _, m := range c.msgs {
		t, _ := gamemsg.TypeOf(m[0])
		out = append(out, t)
	}
	return out
}

func (c *fakeConn) last(t gamemsg.Type) *hlapi.Reader {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := len(c.msgs) - 1; i >= 0; i-- {
		if got, _ := gamemsg.TypeOf(c.msgs[i][0]); got == t {
			return hlapi.NewReader(c.msgs[i][1:])
		}
	}
	return nil
}

type dir struct{}

func (dir) Account(tok string) (string, error) {
	acct, ok := strings.CutSuffix(tok, ".ok")
	if !ok {
		return "", errors.New("bad token")
	}
	return acct, nil
}
func (dir) Match(string) (Match, error) { return Match{ID: "match-1", Players: 2}, nil }

func msg(t gamemsg.Type, f func(w *hlapi.Writer)) []byte {
	w := &hlapi.Writer{}
	w.Start(t.MsgID())
	f(w)
	return w.B
}

func connectInitial() []byte {
	var pv [8]byte
	binary.LittleEndian.PutUint64(pv[:], gamemsg.ProtocolVersion)
	return msg(gamemsg.ClientConnectInitial, func(w *hlapi.Writer) {
		w.I32(0x03E6EB23)
		w.I32(5 + 3)
		for i := 0; i < 5; i++ {
			w.I32(int32(pv[i]))
		}
		w.String("s")
		w.String("b")
		w.I32(4 + 0x03E6EB23)
		for i := 5; i < 9; i++ {
			if i < 8 {
				w.I32(int32(pv[i]))
			} else {
				w.I32(0)
			}
		}
	})
}

func connectClient(proof, netID uint64, token string) []byte {
	return msg(gamemsg.ClientConnectClient, func(w *hlapi.Writer) {
		w.U64(proof)
		w.U64(netID)
		w.Byte(0)
		gamemsg.ShortString(w, token)
		w.Bool(false)
		w.Byte(0)
		w.Byte(1)
		gamemsg.ShortString(w, "")
		w.Bool(false)
		w.Bool(false)
		gamemsg.ShortString(w, "11.6.1")
		gamemsg.ShortString(w, "v")
	})
}

func setReady(netID uint64, state byte) []byte {
	return msg(gamemsg.ClientSetReady, func(w *hlapi.Writer) {
		w.U64(netID)
		w.Byte(state)
		if state == 2 || state == 6 {
			w.String("")
		}
	})
}

func waitFor(t *testing.T, c *fakeConn, typ gamemsg.Type) *hlapi.Reader {
	t.Helper()
	for end := time.Now().Add(3 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		if r := c.last(typ); r != nil {
			return r
		}
	}
	t.Fatalf("no message %d; got %v", typ, c.types())
	return nil
}

func TestTwoPlayersThroughOneRound(t *testing.T) {
	var now float32
	var clock sync.Mutex
	level := Level{RoundID: "r", Scene: "s", Duration: 60, QualifyPct: 50,
		Starts: []Start{{Pos: [3]float32{0, 0, 0}}, {Pos: [3]float32{2, 0, 0}}},
		Finish: []Box{{Center: [3]float32{0, 0, 100}, Half: [3]float32{10, 5, 2}}}}
	s := New(Config{Directory: dir{}, Level: level, Now: func() float32 { clock.Lock(); defer clock.Unlock(); return now }})

	a, b := &fakeConn{}, &fakeConn{}
	for i, c := range []*fakeConn{a, b} {
		s.Connected(c)
		s.Data(c, connectInitial())
		r := waitFor(t, c, gamemsg.ServerConnectInitial)
		k := r.I32()
		for j := int32(0); j < k; j++ {
			r.F32()
		}
		key := r.U64()
		s.Data(c, connectClient(gamemsg.ConnectProof(key), uint64(100+i), []string{"acctA.ok", "acctB.ok"}[i]))
		r = waitFor(t, c, gamemsg.ServerConnectedClient)
		if code := r.I32(); code != 0 {
			t.Fatalf("player %d refused: %d", i, code)
		}
	}
	waitFor(t, a, gamemsg.ServerStartLoadingLevel)
	for i, c := range []*fakeConn{a, b} {
		s.Data(c, append(setReady(uint64(100+i), 1), setReady(uint64(100+i), 2)...))
		s.Data(c, msg(gamemsg.ClientRequestSpawn, func(w *hlapi.Writer) { w.Packed(uint32(i + 1)); gamemsg.ShortString(w, "Bean"); w.Byte(0) }))
	}
	r := waitFor(t, b, gamemsg.ServerSpawnObject)
	_ = r
	spawns, auth := 0, 0
	b.mu.Lock()
	for _, m := range b.msgs {
		if typ, _ := gamemsg.TypeOf(m[0]); typ == gamemsg.ServerSpawnObject {
			spawns++
			if m[1] == 1 {
				auth++
			}
		}
	}
	b.mu.Unlock()
	if spawns != 2 || auth != 1 {
		t.Fatalf("b got %d spawns, %d with authority", spawns, auth)
	}
	for i, c := range []*fakeConn{a, b} {
		s.Data(c, setReady(uint64(100+i), 3))
	}
	waitFor(t, a, gamemsg.ServerEventGeneric)
	for i, c := range []*fakeConn{a, b} {
		s.Data(c, append(setReady(uint64(100+i), 5), setReady(uint64(100+i), 6)...))
	}
	r = waitFor(t, a, gamemsg.ServerStartGame)
	if st, end := r.F32(), r.F32(); st != 3 || end != 63 {
		t.Fatalf("start %v end %v", st, end)
	}

	// Ping is answered with the server clock.
	s.Data(a, msg(gamemsg.Ping, func(w *hlapi.Writer) { w.F32(1); w.F32(2); w.U32(3); w.I32(0) }))
	waitFor(t, a, gamemsg.Pong)

	// a reaches the finish; then time runs out.
	s.mu.Lock()
	pa := s.players[a]
	s.mu.Unlock()
	pa.g.moved(pa, [3]float32{1, 0, 99})
	waitFor(t, b, gamemsg.ServerPlayerProgress)
	clock.Lock()
	now = 64
	clock.Unlock()
	r = waitFor(t, a, gamemsg.ServerEndRound)
	if ep := r.Byte(); ep != gamemsg.EpisodeComplete {
		t.Fatalf("a's episode status %d", ep)
	}
	if ep := waitFor(t, b, gamemsg.ServerEndRound).Byte(); ep != gamemsg.EpisodeFail {
		t.Fatalf("b's episode status %d", ep)
	}
	waitFor(t, b, gamemsg.ServerRoundResults)
}

func TestWrongProofIsRefused(t *testing.T) {
	s := New(Config{Directory: dir{}})
	c := &fakeConn{}
	s.Connected(c)
	s.Data(c, connectInitial())
	waitFor(t, c, gamemsg.ServerConnectInitial)
	s.Data(c, connectClient(1, 1, "x.ok"))
	if code := waitFor(t, c, gamemsg.ServerConnectedClient).I32(); code != gamemsg.ConnectFailedCheck {
		t.Fatalf("code %d", code)
	}
}

func TestFinishBoxRotates(t *testing.T) {
	b := Box{Center: [3]float32{10, 0, 0}, Half: [3]float32{1, 1, 5}, Yaw: 90}
	if !b.inside([3]float32{14, 0, 0}) || b.inside([3]float32{10, 0, 4}) {
		t.Fatal("rotated box")
	}
}
