package main

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestMatchmakerPairsTwoAccounts(t *testing.T) {
	m := newMatchmaker(2, time.Hour, "10.0.0.1", 23533)
	a, b := m.enqueue("aaaa"), m.enqueue("bbbb")
	for _, tk := range []*mmTicket{a, b} {
		select {
		case mt := <-tk.done:
			if len(mt.Players) != 2 || m.get(mt.ID) == nil {
				t.Fatalf("match %+v", mt)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("no match formed")
		}
	}
}

func TestMMSTicketRoundTrip(t *testing.T) {
	s := &server{secret: []byte("k")}
	p, sig := s.mmsTicket("acc-1")
	if a, err := s.checkMMSTicket("Epic-Signed mms-player " + p + " " + sig); err != nil || a != "acc-1" {
		t.Fatalf("%v %v", a, err)
	}
	if _, err := s.checkMMSTicket("Epic-Signed mms-player " + p + " x"); err == nil {
		t.Fatal("forged signature accepted")
	}
}

func TestInternalMatchWantsTheSecret(t *testing.T) {
	s := &server{secret: []byte("s3")}
	s.mm = &matchmaker{matches: map[string]*match{}, byAccount: map[string]*match{}}
	mt := &match{ID: "m1", Players: []string{"a", "b"}}
	s.mm.matches["m1"], s.mm.byAccount["a"] = mt, mt
	for _, c := range []struct {
		auth, acct string
		code       int
	}{{"", "a", 401}, {"Bearer nope", "a", 401}, {"Bearer s3", "zz", 404}, {"Bearer s3", "a", 200}} {
		req := httptest.NewRequest("GET", "/internal/v1/match?account="+c.acct, nil)
		req.Header.Set("Authorization", c.auth)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code != c.code {
			t.Errorf("%q %q: %d, want %d", c.auth, c.acct, rec.Code, c.code)
		}
	}
}
