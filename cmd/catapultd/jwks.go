package main

// Checking an eosd token: RS256 JWTs whose keys eosd publishes as a JWKS. The set is fetched
// on first use and again when a token names a key id we have not seen (eosd restarted with a
// new key), at most once a minute.

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

type keySet struct {
	url     string
	mu      sync.Mutex
	keys    map[string]*rsa.PublicKey
	fetched time.Time
	client  *http.Client
}

func newKeySet(url string) *keySet {
	return &keySet{url: url, keys: map[string]*rsa.PublicKey{}, client: &http.Client{Timeout: 5 * time.Second}}
}

var b64 = base64.RawURLEncoding

func (k *keySet) key(kid string) (*rsa.PublicKey, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if pk, ok := k.keys[kid]; ok {
		return pk, nil
	}
	if time.Since(k.fetched) < time.Minute {
		return nil, fmt.Errorf("unknown key %q", kid)
	}
	k.fetched = time.Now()
	resp, err := k.client.Get(k.url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var set struct {
		Keys []struct{ Kid, N, E string } `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&set); err != nil {
		return nil, err
	}
	for _, j := range set.Keys {
		n, err1 := b64.DecodeString(j.N)
		e, err2 := b64.DecodeString(j.E)
		if err1 != nil || err2 != nil {
			continue
		}
		k.keys[j.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(e).Int64())}
	}
	if pk, ok := k.keys[kid]; ok {
		return pk, nil
	}
	return nil, fmt.Errorf("unknown key %q", kid)
}

// verify checks signature and expiry and returns the claims.
func (k *keySet) verify(tok string) (map[string]any, error) {
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		return nil, errors.New("not a JWT")
	}
	var hdr struct{ Alg, Kid string }
	raw, err := b64.DecodeString(parts[0])
	if err != nil || json.Unmarshal(raw, &hdr) != nil || hdr.Alg != "RS256" {
		return nil, errors.New("bad header")
	}
	pk, err := k.key(hdr.Kid)
	if err != nil {
		return nil, err
	}
	sig, err := b64.DecodeString(parts[2])
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(pk, crypto.SHA256, sum[:], sig); err != nil {
		return nil, errors.New("bad signature")
	}
	raw, err = b64.DecodeString(parts[1])
	if err != nil {
		return nil, err
	}
	var c map[string]any
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, err
	}
	if exp, ok := c["exp"].(float64); !ok || time.Now().Unix() > int64(exp) {
		return nil, errors.New("expired")
	}
	return c, nil
}
