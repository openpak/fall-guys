package round

import (
	"crypto/rand"
	"log"
	"math"
	"sort"
	"sync"
	"time"

	"openpak/fall-guys/internal/gamemsg"
	"openpak/fall-guys/internal/hlapi"
)

// Level is what the server knows about one round: which CMS round it is, where players start
// and what counts as the finish. Positions are world space, metres; yaw in degrees.
type Level struct {
	RoundID       string // a round id of the content document
	Scene         string
	Duration      float32 // seconds
	QualifyPct    uint32
	PlayerPrefab  int32 // hash of the fall guy prefab's name
	Starts        []Start
	Finish        []Box
	EliminationsY float32 // falling below this height eliminates (0: none)
}

type Start struct {
	Pos [3]float32
	Yaw float32
}

// Box is an oriented box: centre, half extents, rotation about Y.
type Box struct {
	Center, Half [3]float32
	Yaw          float32
}

type phase int

const (
	phaseJoining phase = iota
	phaseLoading       // options sent, waiting for spawn requests
	phaseSpawned       // players spawned, waiting for ObjectsSpawned
	phaseIntro         // intro started, waiting for ReadyToPlay
	phasePlaying
	phaseOver
)

type game struct {
	s     *Server
	match Match
	level Level

	mu       sync.Mutex
	phase    phase
	since    time.Time
	players  []*player
	nextID   uint32
	startAt  float32 // server clock when START_GAME went out
	tick     uint32
	guid     gamemsg.Guid
	finished int
	done     chan struct{}
}

func newGame(s *Server, m Match) *game {
	g := &game{s: s, match: m, level: s.cfg.Level, since: time.Now(), nextID: 1, done: make(chan struct{})}
	_, _ = rand.Read(g.guid[:])
	go g.run()
	log.Printf("[round] match %s: new round %s for %d player(s)", short(m.ID), g.level.RoundID, m.Players)
	return g
}

func short(s string) string { return s[:min(8, len(s))] }

func (g *game) started() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.phase != phaseJoining
}

func (g *game) add(p *player) {
	g.mu.Lock()
	p.g = g
	p.playerID = g.nextID
	p.objectID = g.nextID
	g.nextID++
	g.players = append(g.players, p)
	n := len(g.players)
	g.mu.Unlock()
	p.conn.SendReliable(gamemsg.ConnectedClient{
		Code: gamemsg.ConnectParticipant, ServerID: 1, PlayerIDs: []uint32{p.playerID}, Session: g.guid,
		SessionID: g.match.ID, BuildInfo: "openpak", ServerTime: gamemsg.DotNetTicks(time.Now().UnixNano()),
	}.Encode())
	log.Printf("[round] match %s: %s joined as player %d (%d/%d)", short(g.match.ID), p.tag(), p.playerID, n, g.match.Players)
}

func (g *game) leave(p *player) {
	g.mu.Lock()
	p.gone = true
	left := 0
	for _, q := range g.players {
		if !q.gone {
			left++
		}
	}
	g.mu.Unlock()
	log.Printf("[round] match %s: %s left, %d still in", short(g.match.ID), p.tag(), left)
}

// live is every connected player; call with mu held.
func (g *game) live() []*player {
	var out []*player
	for _, p := range g.players {
		if !p.gone {
			out = append(out, p)
		}
	}
	return out
}

func (g *game) broadcast(b []byte) {
	for _, p := range g.live() {
		p.conn.SendReliable(b)
	}
}

func (g *game) setReady(p *player, state byte) {
	g.mu.Lock()
	if state > p.ready {
		p.ready = state
	}
	g.mu.Unlock()
}

func (g *game) requestSpawn(p *player, rs gamemsg.RequestSpawn) {
	g.mu.Lock()
	p.spawnAsk = true
	p.name = rs.Name
	g.mu.Unlock()
}

// clock is the round's time as the clients count it: -5 at START_GAME, 0 when the race
// starts. Before START_GAME it reads -5. Call with mu held.
func (g *game) clock() float32 {
	if g.phase < phasePlaying {
		return -5
	}
	return g.s.cfg.Now() - g.startAt - 5
}

func (g *game) now() float32 {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.clock()
}

// motorTasks reads a client's movement: the newest snapshot says where its fall guy is, and
// the other players get it as that player's motor agent state.
func (g *game) motorTasks(p *player, r *hlapi.Reader) error {
	_, snaps, err := gamemsg.DecodeMotorTasks(r)
	if err != nil || len(snaps) == 0 {
		return err
	}
	s := snaps[len(snaps)-1]
	g.moved(p, s.Transform.Position)
	g.mu.Lock()
	defer g.mu.Unlock()
	g.tick++
	msg := gamemsg.AgentState(p.objectID, s.LocalFrame, g.tick, g.clock(), s.TransformBytes, true)
	for _, q := range g.live() {
		if q != p {
			q.conn.SendUnreliable(msg)
		}
	}
	return nil
}

func (g *game) run() {
	t := time.NewTicker(100 * time.Millisecond)
	defer t.Stop()
	defer g.s.forget(g)
	for range t.C {
		if !g.step() {
			return
		}
	}
}

// all reports whether every live player satisfies f, and that there is at least one.
func (g *game) all(f func(*player) bool) bool {
	live := g.live()
	for _, p := range live {
		if !f(p) {
			return false
		}
	}
	return len(live) > 0
}

// step advances the round when its condition holds or its wait ran out; false ends the game.
func (g *game) step() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	cfg := g.s.cfg
	waited := time.Since(g.since)
	live := g.live()
	if len(live) == 0 && (g.phase != phaseJoining || waited > cfg.JoinWait) {
		log.Printf("[round] match %s: everyone left", short(g.match.ID))
		return false
	}
	next := func(ph phase) { g.phase, g.since = ph, time.Now() }

	switch g.phase {
	case phaseJoining:
		if len(g.players) >= g.match.Players || (len(live) > 0 && waited > cfg.JoinWait) {
			g.startLoading()
			next(phaseLoading)
		}
	case phaseLoading:
		if g.all(func(p *player) bool { return p.spawnAsk }) || waited > cfg.StepWait {
			g.spawnPlayers()
			next(phaseSpawned)
		}
	case phaseSpawned:
		if g.all(func(p *player) bool { return p.ready >= gamemsg.ReadyObjectsSpawned }) || waited > cfg.StepWait {
			g.broadcast(gamemsg.Event(gamemsg.EventStartIntroCameras, gamemsg.Payload{}))
			next(phaseIntro)
		}
	case phaseIntro:
		if g.all(func(p *player) bool { return p.ready >= gamemsg.ReadyReadyToPlay }) || waited > cfg.StepWait {
			// The client's round clock starts at -5 when the level loads and runs on its own:
			// start at 0 gives the usual five-second countdown.
			g.startAt = cfg.Now()
			g.broadcast(gamemsg.StartGame(0, g.level.Duration, byte(len(live)), g.guid))
			log.Printf("[round] match %s: round started, %d player(s)", short(g.match.ID), len(live))
			next(phasePlaying)
		}
	case phasePlaying:
		if g.clock() >= g.level.Duration || g.all(func(p *player) bool { return p.qualified }) {
			g.endRound()
			next(phaseOver)
		}
	case phaseOver:
		if waited > 20*time.Second {
			for _, p := range live {
				p.conn.Disconnect("show over")
			}
			return false
		}
	}
	return true
}

func (g *game) startLoading() {
	live := g.live()
	l := g.level
	opts := gamemsg.Options{
		Duration: l.Duration, RoundID: l.RoundID, SceneName: l.Scene, Seed: int32(g.guid[0]),
		MinParticipants: 1, MaxParticipants: uint32(max(len(live), g.match.Players)),
		CurrentParticipants: uint32(len(live)), QualificationPercent: l.QualifyPct,
		IsFinalRound: true, InitialRoundPlayers: uint32(len(live)),
	}
	g.broadcast(opts.Encode())
	g.broadcast(gamemsg.Empty(gamemsg.ServerStartLoadingLevel))
	log.Printf("[round] match %s: loading %s (%s)", short(g.match.ID), l.RoundID, l.Scene)
}

func (g *game) spawnPlayers() {
	live := g.live()
	sort.Slice(live, func(i, j int) bool { return live[i].playerID < live[j].playerID })
	for i, p := range live {
		st := Start{}
		if n := len(g.level.Starts); n > 0 {
			st = g.level.Starts[i%n]
		}
		p.pos = st.Pos
		sp := gamemsg.Spawn{
			NetID: p.objectID, Type: gamemsg.SpawnPlayer, PrefabHash: g.level.PlayerPrefab,
			Position: st.Pos, Yaw: st.Yaw,
			Player: gamemsg.Player{
				OwnerNetworkID: p.netID, PlayerID: p.playerID, AccountID: p.account,
				PlatformID: "switch", PlatformName: p.name, GeneratedName: p.name,
				SquadID: 0, TeamID: -1, VsGroupID: -1,
			},
		}
		for _, to := range live {
			sp.IsAuth = to == p
			to.conn.SendReliable(sp.Encode())
		}
	}
	g.broadcast(gamemsg.Event(gamemsg.EventQueuedObjectsSpawn, gamemsg.Payload{}))
	g.broadcast(gamemsg.Event(gamemsg.EventAllPlayersSpawned, gamemsg.Payload{}))
	log.Printf("[round] match %s: %d player(s) spawned", short(g.match.ID), len(live))
}

// qualify marks p as through; call with mu held.
func (g *game) qualify(p *player) {
	if p.qualified {
		return
	}
	p.qualified = true
	g.finished++
	ms := int32(g.clock() * 1000)
	g.broadcast(gamemsg.Progress{PlayerID: p.playerID, Succeeded: true, QualifyTime: ms}.Encode())
	g.broadcast(gamemsg.Qualification(uint32(g.finished), 0))
	log.Printf("[round] match %s: player %d qualified (%d)", short(g.match.ID), p.playerID, g.finished)
}

func (g *game) endRound() {
	live := g.live()
	var states []gamemsg.PlayerState
	var results []gamemsg.Result
	pos := uint32(1)
	for _, p := range g.players {
		st := byte(gamemsg.PlayerFailed)
		if p.qualified {
			st = gamemsg.PlayerSucceeded
		}
		states = append(states, gamemsg.PlayerState{PlayerID: p.playerID, State: st})
		results = append(results, gamemsg.Result{NetID: p.objectID, PlayerID: p.playerID, Position: pos, Succeeded: p.qualified})
		pos++
	}
	reason := "round over"
	g.broadcast(gamemsg.Event(gamemsg.EventServerEndingRound, gamemsg.Payload{Str1: &reason}))
	for _, p := range live {
		ep := byte(gamemsg.EpisodeFail)
		if p.qualified {
			ep = gamemsg.EpisodeComplete
		}
		p.conn.SendReliable(gamemsg.EndRound(ep, states))
	}
	g.broadcast(gamemsg.RoundResults(true, false, results))
	log.Printf("[round] match %s: round over, %d qualified of %d", short(g.match.ID), g.finished, len(g.players))
}

// inside reports whether pos is in b.
func (b Box) inside(pos [3]float32) bool {
	dx, dz := pos[0]-b.Center[0], pos[2]-b.Center[2]
	s, c := sincos(-b.Yaw)
	x, z := dx*c-dz*s, dx*s+dz*c
	return abs(x) <= b.Half[0] && abs(pos[1]-b.Center[1]) <= b.Half[1] && abs(z) <= b.Half[2]
}

// moved records where p's fall guy is; call from the movement decoder.
func (g *game) moved(p *player, pos [3]float32) {
	g.mu.Lock()
	defer g.mu.Unlock()
	p.pos = pos
	if g.phase != phasePlaying || p.qualified {
		return
	}
	for _, b := range g.level.Finish {
		if b.inside(pos) {
			g.qualify(p)
			return
		}
	}
}

func sincos(deg float32) (float32, float32) {
	s, c := math.Sincos(float64(deg) * math.Pi / 180)
	return float32(s), float32(c)
}

func abs(f float32) float32 { return float32(math.Abs(float64(f))) }
