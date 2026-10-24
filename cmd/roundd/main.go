// roundd is the Fall Guys round server: the UDP host the client joins once matchmaking hands
// it an address. Hazel transport (internal/hazel), the client's HLAPI framing (internal/hlapi),
// the game messages (internal/gamemsg) and the round rules (internal/round).
//
// Entry tokens are catapultd's: "<account>.<mac>" under the shared CATAPULT_SECRET. Which match
// an account is in comes from catapultd's internal endpoint.
package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"openpak/fall-guys/internal/hazel"
	"openpak/fall-guys/internal/round"
)

func main() {
	addr := envOr("ROUND_ADDR", ":23533")
	secret := os.Getenv("CATAPULT_SECRET")
	if secret == "" {
		log.Fatal("CATAPULT_SECRET is empty: entry tokens cannot be checked")
	}
	level, ok := round.Levels[envOr("ROUND_LEVEL", round.DefaultLevel)]
	if !ok {
		log.Fatalf("unknown ROUND_LEVEL")
	}
	d := &directory{secret: []byte(secret), catapult: envOr("CATAPULT_INTERNAL_URL", "http://127.0.0.1:23530"),
		client: &http.Client{Timeout: 5 * time.Second}}
	s := round.New(round.Config{Directory: d, Level: level})
	l, err := hazel.Listen(addr, adapter{s})
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("[round] listening on udp %s, round %s", l.Addr(), level.RoundID)
	select {}
}

// adapter turns hazel events into round calls.
type adapter struct{ s *round.Server }

func (a adapter) Connected(c *hazel.Conn, _ []byte) {
	log.Printf("[round] transport up: %s (hazel id %d)", c.Addr(), c.ID)
	a.s.Connected(c)
}
func (a adapter) Data(c *hazel.Conn, p []byte, _ bool) { a.s.Data(c, p) }
func (a adapter) Disconnected(c *hazel.Conn, why string) {
	log.Printf("[round] transport down: %s: %s", c.Addr(), why)
	a.s.Disconnected(c)
}

type directory struct {
	secret   []byte
	catapult string
	client   *http.Client
}

// Account checks a catapultd entry token: "<account>.<base64url hmac('entry\0'+account)>".
func (d *directory) Account(tok string) (string, error) {
	acct, sig, ok := strings.Cut(tok, ".")
	if !ok {
		return "", errors.New("entry token has no signature")
	}
	m := hmac.New(sha256.New, d.secret)
	m.Write([]byte("entry\x00" + acct))
	if !hmac.Equal([]byte(base64.RawURLEncoding.EncodeToString(m.Sum(nil))), []byte(sig)) {
		return "", errors.New("entry token signature does not match")
	}
	return acct, nil
}

func (d *directory) Match(acct string) (round.Match, error) {
	req, _ := http.NewRequest("GET", d.catapult+"/internal/v1/match?account="+url.QueryEscape(acct), nil)
	req.Header.Set("Authorization", "Bearer "+string(d.secret))
	resp, err := d.client.Do(req)
	if err != nil {
		return round.Match{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return round.Match{}, fmt.Errorf("catapultd: %s", resp.Status)
	}
	var m struct {
		MatchID string   `json:"matchId"`
		Players []string `json:"players"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return round.Match{}, err
	}
	return round.Match{ID: m.MatchID, Players: len(m.Players)}, nil
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
