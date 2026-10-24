// catapultd answers Fall Guys' account backend on Switch: the HTTPS login service the client
// calls before it opens its gateway socket.
//
// Login only, for now. The client posts {type, token, ...} to /api/v1/login. Type is
// "EosSignIn" and token is the player's EOS Auth (Epic account) access token. On an OpenPak
// console eosd minted that token, so catapultd checks it against eosd's published keys. The
// answer carries a session token, the account id, and where to fetch the content (CMS)
// document. Anything else is logged with its shape and refused: each such line is the
// specification of the next handler.
package main

import (
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"openpak/fall-guys/internal/catapult"
	"openpak/fall-guys/internal/cms"
)

func main() {
	addr := envOr("CATAPULT_ADDR", ":23530")
	s := &server{
		keys:   newKeySet(envOr("EOSD_JWKS_URL", "http://127.0.0.1:22620/auth/v1/oauth/jwks")),
		secret: []byte(os.Getenv("CATAPULT_SECRET")),
		// Plain http: the content downloader checks the server certificate against Mono's own
		// store, not the console's, so it refuses the OpenPak CA (measured on Harbor). The
		// document is public; nothing in it needs TLS.
		contentURL: envOr("CATAPULT_CONTENT_BASE", "http://login.fallguys.oncatapult.com") + contentPath + cms.Version(),
		contentVer: envOr("CATAPULT_CONTENT_VERSION", cms.Version()),
	}
	var err error
	if s.schema, err = catapult.LoadSchema(); err != nil {
		log.Fatalf("schema: %v", err)
	}
	if s.replies, err = catapult.Replies(); err != nil {
		log.Fatalf("rpc table: %v", err)
	}
	s.handlers = gatewayHandlers()
	s.mmsURL = envOr("CATAPULT_MMS_URL", "wss://gateway.fallguys.oncatapult.com/mms")
	minPlayers, _ := strconv.Atoi(envOr("CATAPULT_MIN_PLAYERS", "2"))
	maxWait, _ := time.ParseDuration(envOr("CATAPULT_MAX_WAIT", "30s"))
	port, _ := strconv.Atoi(envOr("ROUND_SERVER_PORT", "23533"))
	s.mm = newMatchmaker(minPlayers, maxWait, envOr("ROUND_SERVER_IP", "127.0.0.1"), port)
	if len(s.secret) == 0 {
		log.Fatal("CATAPULT_SECRET is required (signs session tokens)")
	}
	h := http.NewServeMux()
	h.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })
	h.HandleFunc(contentPath, serveContent)
	h.Handle("/", s)
	srv := &http.Server{Addr: addr, Handler: h, ReadHeaderTimeout: 10 * time.Second}
	log.Printf("[catapult] listening on %s", addr)
	log.Fatal(srv.ListenAndServe())
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
