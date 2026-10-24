package main

import (
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
