package main

// The content (CMS) document. The login answer's contentUrl points here; the client fetches it
// with a plain GET (Accept-Encoding: gzip), gunzips it itself only when the answer says
// Content-Encoding: gzip, and parses it as JSON. It caches the document on disk under the
// login's contentVersion, so the version changes whenever the document does (internal/cms).

import (
	"log"
	"net/http"
	"strconv"

	"openpak/fall-guys/internal/cms"
)

const contentPath = "/content/"

func serveContent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	body := cms.Document()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("ETag", strconv.Quote(cms.Version()))
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	log.Printf("[catapult] content %s -> %d bytes (version %s)", r.URL.Path, len(body), cms.Version())
	if r.Method == http.MethodGet {
		_, _ = w.Write(body)
	}
}
