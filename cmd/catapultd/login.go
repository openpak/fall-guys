package main

// POST /api/v1/login. Property names are the client's own [JsonProperty] names (Catapult
// client SDK, HttpLoginRequest / HttpLoginResponse); see docs/protocol.md.

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

const sessionTTL = 12 * time.Hour

type server struct {
	keys       *keySet
	secret     []byte
	contentURL string
	contentVer string
}

type loginRequest struct {
	Type                   string         `json:"type"`
	Token                  string         `json:"token"`
	Properties             map[string]any `json:"properties"`
	UserParameters         map[string]any `json:"userParameters"`
	ClientVersion          string         `json:"clientVersion"`
	ClientVersionSignature string         `json:"clientVersionSignature"`
	Platform               string         `json:"platform"`
	ContentBranch          string         `json:"contentBranch"`
}

type identity struct {
	Type      string `json:"type"`
	Token     string `json:"token"`
	ID        string `json:"id"`
	AccountID string `json:"accountId"`
}

type loginResponse struct {
	Token                string         `json:"token"`
	Expiry               string         `json:"expiry"`
	AccountID            string         `json:"accountId"`
	Identities           []identity     `json:"identities"`
	Roles                []string       `json:"roles"`
	ContentURL           string         `json:"contentUrl"`
	ContentVersion       string         `json:"contentVersion"`
	ContentETag          string         `json:"contentEtag"`
	AdditionalProperties map[string]any `json:"additionalProperties"`
	TokenType            string         `json:"tokenType"`
	ClientFeatures       []string       `json:"clientFeatures"`
	SocialBans           []any          `json:"socialBans"`
}

// apiError is the error body the client's error handler reads: {title, errorCode, detail}.
type apiError struct {
	Title     string `json:"title"`
	ErrorCode string `json:"errorCode"`
	Detail    string `json:"detail"`
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/login":
		s.login(w, r)
	default:
		s.unhandled(w, r)
	}
}

func (s *server) login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeError(w, 400, "BadRequest", "body is not a login request")
		return
	}
	log.Printf("[catapult] login type=%s platform=%s clientVersion=%s branch=%s props=%s params=%s",
		req.Type, req.Platform, req.ClientVersion, req.ContentBranch, keys(req.Properties), keys(req.UserParameters))
	if req.Type != "EosSignIn" {
		writeError(w, 400, "UnsupportedIdentityType", "only EosSignIn is accepted")
		return
	}
	claims, err := s.keys.verify(req.Token)
	if err != nil {
		log.Printf("[catapult] login refused: %v", err)
		writeError(w, 401, "InvalidIdentityToken", "token not valid")
		return
	}
	epicID, _ := claims["sub"].(string)
	if epicID == "" {
		writeError(w, 401, "InvalidIdentityToken", "token names no account")
		return
	}
	account := s.accountID(epicID)
	exp := time.Now().Add(sessionTTL)
	resp := loginResponse{
		Token:                s.sessionToken(account, exp),
		Expiry:               exp.UTC().Format(time.RFC3339),
		AccountID:            account,
		Identities:           []identity{{Type: "EosSignIn", ID: epicID, AccountID: account}},
		Roles:                []string{},
		ContentURL:           s.contentURL,
		ContentVersion:       s.contentVer,
		ContentETag:          s.contentVer,
		AdditionalProperties: map[string]any{},
		TokenType:            "Bearer",
		ClientFeatures:       []string{},
		SocialBans:           []any{},
	}
	log.Printf("[catapult] login ok account=%s", account[:8])
	writeJSON(w, 200, resp)
}

// accountID is stable per Epic account id and needs no database: an HMAC, shown as a UUID.
func (s *server) accountID(epicID string) string {
	m := hmac.New(sha256.New, s.secret)
	m.Write([]byte("account\x00" + epicID))
	h := hex.EncodeToString(m.Sum(nil)[:16])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

// sessionToken is "<account>.<expiry unix>.<mac>", checked by the gateway when it exists.
func (s *server) sessionToken(account string, exp time.Time) string {
	body := account + "." + strconv.FormatInt(exp.Unix(), 10)
	m := hmac.New(sha256.New, s.secret)
	m.Write([]byte("session\x00" + body))
	return body + "." + base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

func (s *server) unhandled(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<16))
	var shape map[string]any
	_ = json.Unmarshal(body, &shape)
	names := make([]string, 0, len(r.Header))
	for k := range r.Header {
		names = append(names, k)
	}
	sort.Strings(names)
	log.Printf("[catapult] !! unhandled %s %s %s headers=%s body=%s (%d bytes)", r.Method, r.Host, r.URL.Path,
		strings.Join(names, ","), keys(shape), len(body))
	writeError(w, 404, "NotFound", "not implemented")
}

func keys(m map[string]any) string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return "[" + strings.Join(out, ",") + "]"
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, detail string) {
	writeJSON(w, status, apiError{Title: http.StatusText(status), ErrorCode: code, Detail: detail})
}
