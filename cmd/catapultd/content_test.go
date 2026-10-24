package main

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"openpak/fall-guys/internal/cms"
)

func TestContentServesTheDocument(t *testing.T) {
	rec := httptest.NewRecorder()
	serveContent(rec, httptest.NewRequest("GET", contentPath+cms.Version(), nil))
	if rec.Code != 200 || rec.Header().Get("Content-Encoding") != "" {
		t.Fatalf("%d %v", rec.Code, rec.Header())
	}
	var doc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if doc["_meta"].(map[string]any)["_content_version"] != cms.Version() {
		t.Fatal("version mismatch")
	}
}
