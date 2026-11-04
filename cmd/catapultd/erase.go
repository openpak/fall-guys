package main

// Account erasure (card openpak/prds#120). fg_accounts maps a Fall Guys account (an HMAC of the Epic
// account id) to a BAAS user id, so the account's BAAS ids come from the core's Switch links. Erased:
// the fg_accounts rows, the account's entries in the rewards session clock (fg_session_clock), and the
// catapultd memory that names the account (id cache, matchmaking queue and latest match).

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"time"
)

func (s *server) erase(ctx context.Context, account string) (int, error) {
	if s.links == nil {
		return 0, errNoCore
	}
	baas, err := s.links.SwitchSubjects(ctx, account)
	if err != nil {
		return 0, err
	}
	if len(baas) == 0 {
		return 0, nil
	}
	if s.baas == nil {
		return 0, errors.New("account map not configured")
	}
	b := s.baas
	b.mu.Lock()
	db := b.db
	mine := map[string]bool{}
	for _, id := range baas {
		mine[id] = true
	}
	gone := map[string]bool{}
	for acct, v := range b.cache {
		if mine[v] {
			gone[acct] = true
			delete(b.cache, acct)
		}
	}
	b.mu.Unlock()
	n := 0
	if db != nil {
		accts, c, err := db.EraseBaas(ctx, baas)
		if err != nil {
			return 0, err
		}
		n = c
		for _, a := range accts {
			gone[a] = true
		}
	} else {
		n = len(gone)
	}
	if s.mm != nil {
		m := s.mm
		m.mu.Lock()
		kept := m.waiting[:0:0]
		for _, t := range m.waiting {
			if !gone[t.account] {
				kept = append(kept, t)
			}
		}
		m.waiting = kept
		for a := range gone {
			delete(m.byAccount, a)
		}
		m.mu.Unlock()
	}
	return n, nil
}

// serveErase runs the erasure endpoint on its own listener (ERASE_ADDR, default :9099): the gateway
// listener is routed publicly by Traefik, this one is only for the container network.
func (s *server) serveErase() {
	mux := http.NewServeMux()
	mux.HandleFunc("/internal/accounts/", eraseHandler("fall-guys", s.erase))
	addr := os.Getenv("ERASE_ADDR")
	if addr == "" {
		addr = ":9099"
	}
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { log.Printf("[catapult] erase listener: %v", srv.ListenAndServe()) }()
}
