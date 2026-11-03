package main

// Fall Guys online sessions for rewards: game_sessions.fall-guys. The room is the whole match (the show,
// every round of it): a player counts once per match after 3 minutes in it, adding up across rounds,
// with another player in it. Presence is who is actually connected to the round server (Server.Rooms),
// not who matchmaking listed, and a player stays in the match for matchGrace after a drop. Accounts map to BAAS user ids through
// catapultd (/internal/v1/baas, kept in Postgres). The session clock is saved in Postgres
// (FALL_GUYS_DATABASE_URL) after every change and restored at start; Postgres down at start: ERROR
// level, memory only, retried every 30 s (the clock is not restored late).

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"sync"
	"time"

	emit "github.com/openpak/rewards-emit"

	"openpak/fall-guys/internal/round"
	"openpak/fall-guys/internal/store"
)

const sessionMetric = "game_sessions.fall-guys"

// baas asks catapultd which BAAS user id an account is; "" when unknown. Answers are cached.
func (d *directory) baas(account string) string {
	d.mu.Lock()
	if v, ok := d.baasCache[account]; ok {
		d.mu.Unlock()
		return v
	}
	d.mu.Unlock()
	req, _ := http.NewRequest("GET", d.catapult+"/internal/v1/baas?account="+url.QueryEscape(account), nil)
	req.Header.Set("Authorization", "Bearer "+string(d.secret))
	resp, err := d.client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "" // not cached: the player may log in through eosd later
	}
	var o struct {
		Baas string `json:"baas_user_id"`
	}
	if json.NewDecoder(resp.Body).Decode(&o) != nil || o.Baas == "" {
		return ""
	}
	d.mu.Lock()
	if d.baasCache == nil {
		d.baasCache = map[string]string{}
	}
	d.baasCache[account] = o.Baas
	d.mu.Unlock()
	return o.Baas
}

// matchGrace is how long a player stays in their match after their connection drops: a show is several
// rounds, the client leaves the round server between them and joins again, and an eliminated player
// simply does not come back. Longer than the gap between rounds, far shorter than a session.
const matchGrace = 2 * time.Minute

// matches remembers who is in each match across rounds. The room is the whole match (its id is the
// matchmaking match id catapultd and roundd share), so a player's minutes add up over the show.
type matches struct {
	mu   sync.Mutex
	seen map[string]map[string]time.Time // match id -> account -> last time connected
}

func newMatches() *matches { return &matches{seen: map[string]map[string]time.Time{}} }

// observe records who is connected to the round server right now (match id -> accounts) and returns the
// accounts in each match: connected now, or connected within matchGrace. Matches nobody is left in drop.
func (m *matches) observe(now time.Time, connected map[string][]string) map[string][]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, accts := range connected {
		if m.seen[id] == nil {
			m.seen[id] = map[string]time.Time{}
		}
		for _, a := range accts {
			m.seen[id][a] = now
		}
	}
	out := map[string][]string{}
	for id, set := range m.seen {
		for a, t := range set {
			if now.Sub(t) > matchGrace {
				delete(set, a)
				continue
			}
			out[id] = append(out[id], a)
		}
		if len(set) == 0 {
			delete(m.seen, id)
		}
	}
	return out
}

// snapshot turns the matches into rewards rooms; accounts without a BAAS id are left out.
func snapshot(m *matches, s *round.Server, baas func(string) string) []emit.Room {
	return rooms(m.observe(time.Now(), s.Rooms()), baas)
}

func rooms(byMatch map[string][]string, baas func(string) string) []emit.Room {
	var out []emit.Room
	for id, accts := range byMatch {
		r := emit.Room{ID: id}
		for _, a := range accts {
			if b := baas(a); b != "" {
				r.Players = append(r.Players, emit.Identity{Namespace: "switch", Subject: b})
			}
		}
		out = append(out, r)
	}
	return out
}

// clock saves the session clock when it changes; a failed write stays pending for the next tick.
type clock struct {
	mu   sync.Mutex
	db   *store.DB
	last string
	warn time.Time
}

func (c *clock) attach(db *store.DB) { c.mu.Lock(); c.db = db; c.mu.Unlock() }

func (c *clock) save(st emit.State) {
	raw, err := json.Marshal(st)
	if err != nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if string(raw) == c.last {
		return
	}
	if c.db == nil {
		c.complain("session clock store not connected: clock held in memory only", nil)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := c.db.SaveClock(ctx, sessionMetric, raw); err != nil {
		c.complain("saving the session clock failed (will retry)", err)
		return
	}
	c.last = string(raw)
}

func (c *clock) complain(msg string, err error) {
	if time.Since(c.warn) > time.Minute {
		c.warn = time.Now()
		log.Printf("[round] ERROR %s: %v", msg, err)
	}
}

// restore loads the saved clock into sess and attaches the store for later saves.
func (c *clock) restore(ctx context.Context, db *store.DB, sess *emit.Sessions, load bool) error {
	if load {
		raw, err := db.LoadClock(ctx, sessionMetric)
		if err != nil {
			return err
		}
		if raw != nil {
			var st emit.State
			if json.Unmarshal(raw, &st) == nil {
				sess.Restore(st)
				c.mu.Lock()
				c.last = string(raw)
				c.mu.Unlock()
			}
		}
	}
	c.attach(db)
	return nil
}

// startRewards wires the reporter; it does nothing when rewards is not configured.
func startRewards(ctx context.Context, s *round.Server, d *directory, dburl string) {
	rw := emit.FromEnv("fall-guys")
	if !rw.Enabled() {
		log.Printf("[round] rewards events are off (REWARDS_EVENTS_URL/KEY unset)")
		return
	}
	sess := emit.NewSessions(rw, sessionMetric, 0)
	c := &clock{}
	sess.SetOnTick(c.save)
	if dburl == "" {
		log.Printf("[round] FALL_GUYS_DATABASE_URL is empty: the session clock is memory only")
	} else {
		try := func(load bool) error {
			cx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			db, err := store.Open(cx, dburl)
			if err != nil {
				return err
			}
			if err := c.restore(cx, db, sess, load); err != nil {
				db.Close()
				return err
			}
			return nil
		}
		if err := try(true); err != nil {
			log.Printf("[round] ERROR Postgres unavailable, session clock on memory, retrying every 30s: %v", err)
			go func() {
				t := time.NewTicker(30 * time.Second)
				defer t.Stop()
				for {
					select {
					case <-ctx.Done():
						return
					case <-t.C:
						if try(false) == nil {
							log.Printf("[round] Postgres is back")
							return
						}
					}
				}
			}()
		} else {
			log.Printf("[round] session clock persisted in Postgres")
		}
	}
	ms := newMatches()
	sess.Watch(ctx, 20*time.Second, func() []emit.Room { return snapshot(ms, s, d.baas) })
	log.Printf("[round] rewards events are on: %s", sessionMetric)
}
