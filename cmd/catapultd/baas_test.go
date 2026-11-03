package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"openpak/fall-guys/internal/store"
)

func TestAccountMapSurvivesRestart(t *testing.T) {
	url := os.Getenv("FALL_GUYS_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("FALL_GUYS_TEST_DATABASE_URL not set")
	}
	eos := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Internal-Key") != "k" || r.URL.Path != "/internal/v1/owner/epic1" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"baas_user_id":"f00d"}`))
	}))
	defer eos.Close()
	t.Setenv("EOS_INTERNAL_URL", eos.URL)
	t.Setenv("EOS_INTERNAL_KEY", "k")
	ctx := context.Background()

	db, err := store.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	a := newBaasMap()
	a.attach(db)
	a.remember(ctx, "acct-restart-test", "epic1")
	a.remember(ctx, "acct-unknown", "someoneelse") // eosd never issued it: not stored
	if a.baas(ctx, "acct-restart-test") != "f00d" {
		t.Fatal("not remembered")
	}

	eos.Close() // eosd gone; a restarted catapultd still knows
	b := newBaasMap()
	db2, _ := store.Open(ctx, url)
	b.attach(db2)
	if got := b.baas(ctx, "acct-restart-test"); got != "f00d" {
		t.Fatalf("after restart: %q", got)
	}
	if b.baas(ctx, "acct-unknown") != "" {
		t.Fatal("unknown account mapped")
	}
}
