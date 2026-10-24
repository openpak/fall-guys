package main

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func mint(t *testing.T, key *rsa.PrivateKey, claims map[string]any) string {
	t.Helper()
	hdr, _ := json.Marshal(map[string]any{"alg": "RS256", "typ": "JWT", "kid": "k1"})
	body, _ := json.Marshal(claims)
	in := b64.EncodeToString(hdr) + "." + b64.EncodeToString(body)
	sum := sha256.Sum256([]byte(in))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	return in + "." + b64.EncodeToString(sig)
}

func testServer(t *testing.T) (*server, *rsa.PrivateKey) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	jwks := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]any{
			"kid": "k1", "kty": "RSA", "n": b64.EncodeToString(key.N.Bytes()),
			"e": b64.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
		}}})
	}))
	t.Cleanup(jwks.Close)
	return &server{keys: newKeySet(jwks.URL), secret: []byte("s"), contentURL: "http://c/x", contentVer: "v1"}, key
}

func post(s *server, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/login", strings.NewReader(body)))
	return rec
}

func TestLoginWithEosdToken(t *testing.T) {
	s, key := testServer(t)
	tok := mint(t, key, map[string]any{"sub": "epic-1", "exp": time.Now().Add(time.Hour).Unix()})
	rec := post(s, `{"type":"EosSignIn","token":"`+tok+`","clientVersion":"11.6.1","platform":"switch"}`)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	for _, k := range []string{"token", "expiry", "accountId", "identities", "roles", "contentUrl",
		"contentVersion", "contentEtag", "additionalProperties", "tokenType", "clientFeatures", "socialBans"} {
		if _, ok := resp[k]; !ok {
			t.Errorf("response lacks %q", k)
		}
	}
	again := post(s, `{"type":"EosSignIn","token":"`+tok+`"}`)
	var resp2 map[string]any
	_ = json.Unmarshal(again.Body.Bytes(), &resp2)
	if resp["accountId"] != resp2["accountId"] {
		t.Error("account id is not stable for one Epic account")
	}
}

func TestLoginRefusesForeignOrExpiredToken(t *testing.T) {
	s, _ := testServer(t)
	other, _ := rsa.GenerateKey(rand.Reader, 2048)
	bad := mint(t, other, map[string]any{"sub": "x", "exp": time.Now().Add(time.Hour).Unix()})
	if rec := post(s, `{"type":"EosSignIn","token":"`+bad+`"}`); rec.Code != 401 {
		t.Errorf("foreign key: status %d", rec.Code)
	}
	s2, key := testServer(t)
	old := mint(t, key, map[string]any{"sub": "x", "exp": time.Now().Add(-time.Minute).Unix()})
	if rec := post(s2, `{"type":"EosSignIn","token":"`+old+`"}`); rec.Code != 401 {
		t.Errorf("expired: status %d", rec.Code)
	}
	if rec := post(s2, `{"type":"Steam","token":"x"}`); rec.Code != 400 {
		t.Errorf("other type: status %d", rec.Code)
	}
}
