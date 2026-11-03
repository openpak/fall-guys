package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	emit "github.com/openpak/rewards-emit"

	"openpak/fall-guys/internal/store"
)

// Against a real Postgres when FALL_GUYS_TEST_DATABASE_URL is set: the session clock survives a restart.
func TestSessionClockSurvivesRestart(t *testing.T) {
	url := os.Getenv("FALL_GUYS_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("FALL_GUYS_TEST_DATABASE_URL not set")
	}
	var mu sync.Mutex
	var bodies []string
	rewards := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(b))
		mu.Unlock()
	}))
	defer rewards.Close()
	t.Setenv("REWARDS_EVENTS_URL", rewards.URL)
	t.Setenv("REWARDS_EVENTS_KEY", "k")

	ctx := context.Background()
	boot := func(load bool) (*emit.Sessions, *clock) {
		db, err := store.Open(ctx, url)
		if err != nil {
			t.Fatal(err)
		}
		sess := emit.NewSessions(emit.FromEnv("fall-guys"), sessionMetric, 3*time.Minute)
		c := &clock{}
		sess.SetOnTick(c.save)
		if err := c.restore(ctx, db, sess, load); err != nil {
			t.Fatal(err)
		}
		return sess, c
	}
	a, b := emit.Identity{Namespace: "switch", Subject: "aaaa"}, emit.Identity{Namespace: "switch", Subject: "bbbb"}
	room := []emit.Room{{ID: "match-restart-test", Players: []emit.Identity{a, b}}}
	t0 := time.Now()

	first, c1 := boot(false)
	// start from a clean clock
	db, _ := store.Open(ctx, url)
	_ = db.SaveClock(ctx, sessionMetric, []byte(`{}`))
	first.Tick(t0, room)
	c1.save(first.State())                  // what Watch's OnTick does after every tick
	first.Tick(t0.Add(2*time.Minute), room) // 2 of 3 minutes, then the process "dies"
	c1.save(first.State())

	second, c2 := boot(true)
	if n := second.Tick(t0.Add(3*time.Minute), room); n != 2 {
		t.Fatalf("after restart the clock should have counted both players at minute 3, got %d", n)
	}
	c2.save(second.State())
	if n := second.Tick(t0.Add(9*time.Minute), room); n != 0 {
		t.Fatalf("counted twice: %d", n)
	}
	third, _ := boot(true) // another restart: still counted once
	if n := third.Tick(t0.Add(10*time.Minute), room); n != 0 {
		t.Fatalf("counted again after a second restart: %d", n)
	}
	time.Sleep(3 * time.Second)
	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(strings.Join(bodies, ""), "game_sessions.fall-guys:match-restart-test:switch:aaaa") {
		t.Fatalf("event not delivered: %v", bodies)
	}
}
