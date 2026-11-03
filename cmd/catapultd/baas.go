package main

// Which BAAS account a Fall Guys account is, for rewards. The account id is an HMAC of the Epic account
// id and cannot be turned back, so at login catapultd asks eosd (EOS_INTERNAL_URL + EOS_INTERNAL_KEY,
// GET /internal/v1/owner/<id>) who the token's subject is and keeps the answer in Postgres
// (FALL_GUYS_DATABASE_URL, table fg_accounts) with a memory cache in front. roundd reads it back here.

import (
	"context"
	"crypto/hmac"
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"openpak/fall-guys/internal/store"
)

type baasMap struct {
	mu      sync.Mutex
	cache   map[string]string
	db      *store.DB
	eosURL  string
	eosKey  string
	client  *http.Client
	lastErr time.Time
}

func newBaasMap() *baasMap {
	return &baasMap{cache: map[string]string{}, eosURL: strings.TrimRight(os.Getenv("EOS_INTERNAL_URL"), "/"),
		eosKey: os.Getenv("EOS_INTERNAL_KEY"), client: &http.Client{Timeout: 3 * time.Second}}
}

func (b *baasMap) attach(db *store.DB) {
	b.mu.Lock()
	b.db = db
	b.mu.Unlock()
}

func (b *baasMap) logErr(msg string, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if time.Since(b.lastErr) > time.Minute {
		b.lastErr = time.Now()
		log.Printf("[catapult] ERROR %s: %v", msg, err)
	}
}

// remember asks eosd who subject (the EOS token's sub) is and stores account -> BAAS id.
func (b *baasMap) remember(ctx context.Context, account, subject string) {
	if b.eosURL == "" || b.eosKey == "" || subject == "" {
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.eosURL+"/internal/v1/owner/"+url.PathEscape(subject), nil)
	if err != nil {
		return
	}
	req.Header.Set("X-Internal-Key", b.eosKey)
	resp, err := b.client.Do(req)
	if err != nil {
		b.logErr("eosd owner lookup failed", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return // not an account eosd issued: it never counts
	}
	var o struct {
		Baas string `json:"baas_user_id"`
	}
	if json.NewDecoder(resp.Body).Decode(&o) != nil || o.Baas == "" {
		return
	}
	b.mu.Lock()
	b.cache[account] = o.Baas
	db := b.db
	b.mu.Unlock()
	if db != nil {
		if err := db.PutAccount(ctx, account, o.Baas); err != nil {
			b.logErr("saving the account map failed (retried at the next login)", err)
		}
	}
}

// baas returns the BAAS id of an account: cache, then Postgres.
func (b *baasMap) baas(ctx context.Context, account string) string {
	b.mu.Lock()
	v, db := b.cache[account], b.db
	b.mu.Unlock()
	if v != "" || db == nil {
		return v
	}
	v, err := db.Account(ctx, account)
	if err != nil {
		b.logErr("reading the account map failed", err)
		return ""
	}
	if v != "" {
		b.mu.Lock()
		b.cache[account] = v
		b.mu.Unlock()
	}
	return v
}

// startStore connects Postgres when url is set; down at start, it retries every 30 s at ERROR level.
func (b *baasMap) startStore(ctx context.Context, dburl string) {
	if dburl == "" {
		log.Printf("[catapult] FALL_GUYS_DATABASE_URL is empty: the account map is memory only")
		return
	}
	try := func() error {
		c, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		db, err := store.Open(c, dburl)
		if err != nil {
			return err
		}
		b.attach(db)
		return nil
	}
	if err := try(); err == nil {
		log.Printf("[catapult] account map persisted in Postgres")
		return
	} else {
		log.Printf("[catapult] ERROR Postgres unavailable, running on memory and retrying every 30s: %v", err)
	}
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for range t.C {
			if try() == nil {
				log.Printf("[catapult] Postgres is back")
				return
			}
		}
	}()
}

// internalBaas serves GET /internal/v1/baas?account=<id> to roundd (shared secret, like internalMatch).
func (s *server) internalBaas(w http.ResponseWriter, r *http.Request) {
	tok, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !hmac.Equal([]byte(tok), s.secret) {
		http.Error(w, "unauthorized", 401)
		return
	}
	v := s.baas.baas(r.Context(), r.URL.Query().Get("account"))
	if v == "" {
		http.Error(w, "unknown", 404)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"baas_user_id": v})
}
