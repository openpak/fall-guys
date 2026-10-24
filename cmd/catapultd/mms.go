package main

// Matchmaking. The gateway hands the client a signed matchmaking ticket and a websocket URL
// (GetPlayerMatchmakingTokenQuery -> PlayerMatchmakingTokenDto). The client connects there with
// "Authorization: Epic-Signed mms-player <payload> <signature>" and reads JSON
// {"name", "payload"} messages:
//
//   StatusUpdate {state: Connecting | Waiting | Queued | SessionAssignment}, then Play {sessionId,
//   matchId, playerId, joinDelaySec}.
//
// It then asks the gateway for the match's status (GetMatchStatusQuery) until it is Ready with
// the round server's address, and for an entry token (CreateGameEntryTokenCommand).
//
// One queue, no skill or region: a match forms when MinPlayers are queued, or when the oldest
// has waited MaxWait with at least one player.

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

type matchmaker struct {
	minPlayers int
	maxWait    time.Duration
	serverIP   string
	serverPort int

	mu      sync.Mutex
	waiting []*mmTicket
	matches map[string]*match
}

type mmTicket struct {
	account string
	since   time.Time
	done    chan *match
}

type match struct {
	ID, SessionID string
	Players       []string
	Created       time.Time
}

func newMatchmaker(minPlayers int, maxWait time.Duration, ip string, port int) *matchmaker {
	m := &matchmaker{minPlayers: minPlayers, maxWait: maxWait, serverIP: ip, serverPort: port, matches: map[string]*match{}}
	go m.loop()
	return m
}

func (m *matchmaker) enqueue(account string) *mmTicket {
	t := &mmTicket{account: account, since: time.Now(), done: make(chan *match, 1)}
	m.mu.Lock()
	m.waiting = append(m.waiting, t)
	m.mu.Unlock()
	return t
}

func (m *matchmaker) cancel(t *mmTicket) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, w := range m.waiting {
		if w == t {
			m.waiting = append(m.waiting[:i], m.waiting[i+1:]...)
			return
		}
	}
}

func (m *matchmaker) queued() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.waiting)
}

func (m *matchmaker) loop() {
	for range time.Tick(time.Second) {
		m.mu.Lock()
		n := len(m.waiting)
		if n >= m.minPlayers || (n > 0 && time.Since(m.waiting[0].since) >= m.maxWait) {
			mt := &match{ID: randomHex(32), SessionID: randomHex(32), Created: time.Now()}
			for _, t := range m.waiting {
				mt.Players = append(mt.Players, t.account)
			}
			m.matches[mt.ID] = mt
			for _, t := range m.waiting {
				t.done <- mt
			}
			m.waiting = nil
			log.Printf("[mms] match %s formed with %d player(s)", mt.ID[:8], len(mt.Players))
		}
		for id, mt := range m.matches {
			if time.Since(mt.Created) > time.Hour {
				delete(m.matches, id)
			}
		}
		m.mu.Unlock()
	}
}

func (m *matchmaker) get(id string) *match {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.matches[id]
}

// ---- tickets ----

// ticket returns "<payload> <signature>": the payload names the account, the signature is ours.
func (s *server) mmsTicket(account string) (string, string) {
	p, _ := json.Marshal(map[string]any{"account": account, "exp": time.Now().Add(10 * time.Minute).Unix()})
	payload := base64.RawURLEncoding.EncodeToString(p)
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte("mms\x00" + payload))
	return payload, base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (s *server) checkMMSTicket(auth string) (string, error) {
	rest, ok := strings.CutPrefix(auth, "Epic-Signed mms-player ")
	if !ok {
		return "", errors.New("not an Epic-Signed mms-player header")
	}
	payload, sig, ok := strings.Cut(strings.TrimSpace(rest), " ")
	if !ok {
		return "", errors.New("no signature")
	}
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte("mms\x00" + payload))
	if !hmac.Equal([]byte(base64.RawURLEncoding.EncodeToString(mac.Sum(nil))), []byte(sig)) {
		return "", errors.New("bad signature")
	}
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return "", err
	}
	var c struct {
		Account string `json:"account"`
		Exp     int64  `json:"exp"`
	}
	if err := json.Unmarshal(raw, &c); err != nil || c.Account == "" || time.Now().Unix() > c.Exp {
		return "", errors.New("expired or empty ticket")
	}
	return c.Account, nil
}

// ---- the matchmaking socket ----

func (s *server) mms(w http.ResponseWriter, r *http.Request) {
	account, err := s.checkMMSTicket(r.Header.Get("Authorization"))
	if err != nil {
		log.Printf("[mms] refused: %v", err)
		http.Error(w, "unauthorized", 401)
		return
	}
	ws, err := wsUpgrade(w, r)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	defer ws.close()
	send := func(name string, payload map[string]any) {
		p, _ := json.Marshal(map[string]any{"name": name, "payload": payload})
		ws.mu.Lock()
		defer ws.mu.Unlock()
		_ = ws.writeFrameLocked(0x1, p)
	}
	closed := make(chan struct{})
	go func() { // the client sends nothing we need; read only to notice it leaving
		for {
			if _, err := ws.read(); err != nil {
				close(closed)
				return
			}
		}
	}()

	send("StatusUpdate", map[string]any{"state": "Connecting"})
	send("StatusUpdate", map[string]any{"state": "Waiting", "totalPlayers": 1, "connectedPlayers": 1})
	t := s.mm.enqueue(account)
	ticketID := randomHex(32)
	log.Printf("[mms] %s queued", account[:8])
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for {
		send("StatusUpdate", map[string]any{"state": "Queued", "ticketId": ticketID,
			"queuedPlayers": s.mm.queued(), "estimatedWaitSec": int(s.mm.maxWait.Seconds())})
		select {
		case mt := <-t.done:
			send("StatusUpdate", map[string]any{"state": "SessionAssignment", "matchId": mt.ID})
			send("Play", map[string]any{"sessionId": mt.SessionID, "matchId": mt.ID, "playerId": account, "joinDelaySec": 0})
			time.Sleep(time.Second)
			_ = ws.writeFrame(0x8, []byte{0x03, 0xe8}) // normal closure
			return
		case <-closed:
			s.mm.cancel(t)
			log.Printf("[mms] %s left the queue", account[:8])
			return
		case <-tick.C:
		}
	}
}

// ---- gateway side ----

const gwMM = "FallGuys.Gateway.Protocol.Client.Matchmaking."

func getMatchmakingToken(g *gwSession, _ map[string]any) (map[string]any, error) {
	payload, sig := g.s.mmsTicket(g.acct())
	return map[string]any{"Payload": payload, "Signature": sig, "ConnectionString": g.s.mmsURL,
		"MetaDataToken": "", "ServerEntryToken": g.s.entryToken(g.acct())}, nil
}

// entryToken is what the round server accepts from this account.
func (s *server) entryToken(account string) string {
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte("entry\x00" + account))
	return account + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func getMatchStatus(g *gwSession, body map[string]any) (map[string]any, error) {
	id, _ := body["MatchId"].(string)
	mt := g.s.mm.get(id)
	if mt == nil {
		return map[string]any{"MatchStatus": int64(0)}, nil // Unknown
	}
	return map[string]any{"MatchStatus": int64(2), "IpAddress": g.s.mm.serverIP, "Port": int64(g.s.mm.serverPort),
		"GameServerToken": g.s.entryToken(g.acct()), "PlayerCount": int64(len(mt.Players)), "BotCount": int64(0)}, nil
}

func createGameEntryToken(g *gwSession, body map[string]any) (map[string]any, error) {
	ip, _ := body["ServerIp"].(string)
	port, _ := body["ServerPort"].(int64)
	if ip == "" {
		ip, port = g.s.mm.serverIP, int64(g.s.mm.serverPort)
	}
	tok := g.s.entryToken(g.acct())
	return map[string]any{"ServerAddress": ip + ":" + itoa64(port), "ServerIp": ip, "ServerPort": itoa64(port),
		"PartyLeaderEntryToken": tok, "CompanionEntryTokensByIdentity": map[string]any{}}, nil
}
