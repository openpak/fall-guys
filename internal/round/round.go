// Package round is the Fall Guys round server: it takes the players of one match through a
// show, one round at a time. Written from the client's protocol only (docs/protocol.md,
// "Round server"); the client ships no server half to compare with.
//
// The flow for one round, as the client walks it:
//
//	client  CONNECT_INITIAL                 server  CONNECT_INITIAL(key)
//	client  CONNECT_CLIENT(proof, token)    server  CONNECTED_CLIENT(0, player id)
//	(everyone in)                           server  GAME_DATA_OPTIONS, START_LOADING_LEVEL
//	client  SET_READY 1, 2; REQUEST_SPAWN   server  SPAWN_OBJECT per player, QueuedObjectsSpawned,
//	                                                AllPlayersSpawned
//	client  SET_READY 3                     server  StartIntroCameras
//	client  SET_READY 4, 5, 6               server  START_GAME(countdown, end time)
//	client  MOTORTASKS ...                  server  the others' movement, qualification
//	(all done or time up)                   server  ServerEndingRound, END_ROUND, ROUND_RESULTS
//
// Each step waits for every player still connected, with a timeout so one stuck console does
// not hold the others.
package round

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"log"
	"sync"
	"time"

	"openpak/fall-guys/internal/gamemsg"
	"openpak/fall-guys/internal/hlapi"
)

// Conn is one client's transport (internal/hazel.Conn in production).
type Conn interface {
	SendReliable(payload []byte)
	SendUnreliable(payload []byte)
	Disconnect(reason string)
}

// Match is what matchmaking says about a player.
type Match struct {
	ID      string
	Players int // how many the match was formed with
}

// Directory answers who a client is and which match it belongs to.
type Directory interface {
	// Account checks an entry token and names its account.
	Account(entryToken string) (string, error)
	// Match finds the account's current match.
	Match(account string) (Match, error)
}

// Config holds what a server needs to run rounds.
type Config struct {
	Directory Directory
	Level     Level
	// Step timeouts. Zero values take the defaults.
	JoinWait, StepWait time.Duration
	// Clock in seconds; the server's simulation time. Tests replace it.
	Now func() float32
}

// Server owns every match being played.
type Server struct {
	cfg     Config
	mu      sync.Mutex
	players map[Conn]*player
	games   map[string]*game
}

func New(cfg Config) *Server {
	if cfg.JoinWait == 0 {
		cfg.JoinWait = 30 * time.Second
	}
	if cfg.StepWait == 0 {
		cfg.StepWait = 45 * time.Second
	}
	if cfg.Now == nil {
		t0 := time.Now()
		cfg.Now = func() float32 { return float32(time.Since(t0).Seconds()) }
	}
	return &Server{cfg: cfg, players: map[Conn]*player{}, games: map[string]*game{}}
}

type player struct {
	conn      Conn
	key       uint64
	account   string
	netID     uint64 // FG_NetworkID the client named itself
	playerID  uint32
	objectID  uint32 // MPGNetID of its fall guy
	name      string
	ready     byte
	spawnAsk  bool
	qualified bool
	gone      bool
	pos       [3]float32
	g         *game
	defrag    hlapi.Defragmenter
}

// Connected: a client's transport is up. Nothing is sent until it speaks.
func (s *Server) Connected(c Conn) {
	s.mu.Lock()
	s.players[c] = &player{conn: c}
	s.mu.Unlock()
}

// Disconnected: the client is gone.
func (s *Server) Disconnected(c Conn) {
	s.mu.Lock()
	p := s.players[c]
	delete(s.players, c)
	s.mu.Unlock()
	if p != nil && p.g != nil {
		p.g.leave(p)
	}
}

// Data handles one datagram's payload.
func (s *Server) Data(c Conn, payload []byte) {
	s.mu.Lock()
	p := s.players[c]
	s.mu.Unlock()
	if p == nil {
		return
	}
	s.read(p, hlapi.NewReader(payload))
}

func (s *Server) read(p *player, r *hlapi.Reader) {
	for r.More() {
		id := r.Byte()
		if id == hlapi.MsgFragment {
			if msg, done := p.defrag.Add(r); done {
				s.read(p, hlapi.NewReader(msg))
			}
			continue
		}
		t, ok := gamemsg.TypeOf(id)
		if !ok {
			log.Printf("[round] !! unhandled HLAPI message %d (%d bytes left)", id, r.Left())
			return
		}
		if err := s.handle(p, t, r); err != nil {
			log.Printf("[round] %s: message %d: %v", p.tag(), t, err)
			return
		}
	}
}

func (p *player) tag() string {
	if len(p.account) >= 8 {
		return p.account[:8]
	}
	return "new client"
}

func (s *Server) handle(p *player, t gamemsg.Type, r *hlapi.Reader) error {
	switch t {
	case gamemsg.ClientConnectInitial:
		ci, err := gamemsg.DecodeConnectInitial(r)
		if err != nil {
			return err
		}
		if ci.Protocol != gamemsg.ProtocolVersion {
			log.Printf("[round] client protocol %#x, this server speaks %#x", ci.Protocol, gamemsg.ProtocolVersion)
		}
		var b [8]byte
		_, _ = rand.Read(b[:])
		p.key = binary.LittleEndian.Uint64(b[:]) | 1<<40
		log.Printf("[round] connect initial: build %q", ci.Build)
		p.conn.SendReliable(gamemsg.EncodeServerConnectInitial(p.key))

	case gamemsg.ClientConnectClient:
		cc, err := gamemsg.DecodeConnectClient(r)
		if err != nil {
			return err
		}
		s.join(p, cc)

	case gamemsg.ClientSetReady:
		sr, err := gamemsg.DecodeSetReady(r)
		if err != nil {
			return err
		}
		if p.g != nil {
			p.g.setReady(p, sr.State)
		}

	case gamemsg.ClientRequestSpawn:
		rs, err := gamemsg.DecodeRequestSpawn(r)
		if err != nil {
			return err
		}
		if p.g != nil {
			p.g.requestSpawn(p, rs)
		}

	case gamemsg.Ping:
		pg, err := gamemsg.DecodePing(r)
		if err != nil {
			return err
		}
		now := float32(-5)
		if p.g != nil {
			now = p.g.now()
		}
		p.conn.SendUnreliable(gamemsg.EncodePong(pg, 0, now))

	case gamemsg.ClientMotorTasks:
		if p.g == nil {
			r.Pos = len(r.B)
			return nil
		}
		return p.g.motorTasks(p, r)

	case gamemsg.ClientReadyRound:
		r.U64()
		if p.g != nil {
			p.conn.SendReliable(gamemsg.ReadyRoundResponse(2, nil)) // one round per show for now
		}

	case gamemsg.ClientDisconnect:
		r.Pos = len(r.B)
		p.conn.Disconnect("client left")

	default:
		log.Printf("[round] %s: !! unhandled game message %d (%d bytes)", p.tag(), t, r.Left())
		r.Pos = len(r.B) // its length is unknown: the rest of the datagram goes with it
	}
	return nil
}

func (s *Server) join(p *player, cc gamemsg.ConnectClient) {
	refuse := func(code int32, why string) {
		log.Printf("[round] refused (%d): %s", code, why)
		p.conn.SendReliable(gamemsg.ConnectedClient{Code: code}.Encode())
	}
	if cc.Proof != gamemsg.ConnectProof(p.key) {
		refuse(gamemsg.ConnectFailedCheck, "key proof does not match")
		return
	}
	acct, err := s.cfg.Directory.Account(cc.EntryToken)
	if err != nil {
		refuse(gamemsg.ConnectTokenInvalid, err.Error())
		return
	}
	m, err := s.cfg.Directory.Match(acct)
	if err != nil {
		refuse(gamemsg.ConnectSlotNotReserv, fmt.Sprintf("%s: %v", acct[:min(8, len(acct))], err))
		return
	}
	p.account, p.netID = acct, cc.NetworkID

	s.mu.Lock()
	g := s.games[m.ID]
	if g == nil || g.started() {
		g = newGame(s, m)
		s.games[m.ID] = g
	}
	s.mu.Unlock()
	g.add(p)
}

func (s *Server) forget(g *game) {
	s.mu.Lock()
	if s.games[g.match.ID] == g {
		delete(s.games, g.match.ID)
	}
	s.mu.Unlock()
}
