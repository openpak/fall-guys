package main

import (
	"context"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"openpak/fall-guys/internal/store"
)

const (
	acctA = "11111111-1111-4111-8111-111111111111"
	acctB = "22222222-2222-4222-8222-222222222222"
)

type fakeLinks map[string][]string

func (f fakeLinks) SwitchSubjects(_ context.Context, a string) ([]string, error) { return f[a], nil }

func eraseServer() *server {
	m := &matchmaker{matches: map[string]*match{}, byAccount: map[string]*match{}}
	m.waiting = []*mmTicket{{account: "fgA"}, {account: "fgB"}}
	m.byAccount["fgA"], m.byAccount["fgB"] = &match{}, &match{}
	return &server{baas: newBaasMap(), mm: m, links: fakeLinks{acctA: {"baasA"}, acctB: {"baasB"}}}
}

func TestEraseMemory(t *testing.T) {
	s := eraseServer()
	s.baas.cache["fgA"], s.baas.cache["fgB"] = "baasA", "baasB"
	n, err := s.erase(context.Background(), acctA)
	if err != nil || n != 1 {
		t.Fatalf("erase: %d %v", n, err)
	}
	if _, ok := s.baas.cache["fgA"]; ok || s.baas.cache["fgB"] != "baasB" {
		t.Fatalf("cache: %v", s.baas.cache)
	}
	if len(s.mm.waiting) != 1 || s.mm.waiting[0].account != "fgB" || s.mm.byAccount["fgA"] != nil || s.mm.byAccount["fgB"] == nil {
		t.Fatal("matchmaker not scrubbed correctly")
	}
	if n, err := s.erase(context.Background(), acctA); err != nil || n != 0 {
		t.Fatalf("second erase: %d %v", n, err)
	}
}

func TestEraseNeedsCore(t *testing.T) {
	if _, err := (&server{}).erase(context.Background(), acctA); err == nil {
		t.Fatal("erase without a core must fail")
	}
}

// Against a real Postgres (FALL_GUYS_TEST_DATABASE_URL).
func TestEraseDatabase(t *testing.T) {
	dsn := os.Getenv("FALL_GUYS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("FALL_GUYS_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	db, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_ = db.PutAccount(ctx, "fgA", "baasA")
	_ = db.PutAccount(ctx, "fgB", "baasB")
	st := `{"since":{"r` + "\\u0000" + `switch` + "\\u0000" + `baasA":"2026-01-01T00:00:00Z","r` + "\\u0000" + `switch` + "\\u0000" + `baasB":"2026-01-01T00:00:00Z"},"emitted":null}`
	_ = db.SaveClock(ctx, store.SessionMetric, []byte(st))
	s := eraseServer()
	s.baas.attach(db)
	n, err := s.erase(ctx, acctA)
	if err != nil || n != 2 {
		t.Fatalf("erase: %d %v", n, err)
	}
	if v, _ := db.Account(ctx, "fgA"); v != "" {
		t.Fatal("row kept")
	}
	if v, _ := db.Account(ctx, "fgB"); v != "baasB" {
		t.Fatal("other row erased")
	}
	raw, _ := db.LoadClock(ctx, store.SessionMetric)
	if strings.Contains(string(raw), "baasA") || !strings.Contains(string(raw), "baasB") {
		t.Fatalf("clock: %s", raw)
	}
	if n, err := s.erase(ctx, acctA); err != nil || n != 0 {
		t.Fatalf("second erase: %d %v", n, err)
	}
}

func TestEraseEndpoint(t *testing.T) {
	h := eraseHandler("fall-guys", eraseServer().erase)
	call := func(id, auth string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("DELETE", "/internal/accounts/"+id+"/data", nil)
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	t.Setenv("ACCOUNT_ERASE_KEY", "")
	if r := call(acctA, "Bearer k"); r.Code != 503 {
		t.Fatalf("unset key: %d", r.Code)
	}
	t.Setenv("ACCOUNT_ERASE_KEY", "k")
	if r := call(acctA, ""); r.Code != 401 {
		t.Fatalf("no key: %d", r.Code)
	}
	if r := call(acctA, "Bearer bad"); r.Code != 401 {
		t.Fatalf("wrong key: %d", r.Code)
	}
	if r := call("nope", "Bearer k"); r.Code != 400 {
		t.Fatalf("bad id: %d", r.Code)
	}
	if r := call(acctA, "Bearer k"); r.Code != 200 || !strings.Contains(r.Body.String(), `"service":"fall-guys","rows":0`) {
		t.Fatalf("ok: %d %s", r.Code, r.Body.String())
	}
}
