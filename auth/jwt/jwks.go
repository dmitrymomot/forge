package jwt

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/resilience/singleflight"
)

// jwk is the wire form of a single RFC 7517 key.
type jwk struct {
	Kty    string   `json:"kty"`
	Kid    string   `json:"kid,omitempty"`
	Alg    string   `json:"alg,omitempty"`
	Use    string   `json:"use,omitempty"`
	N      string   `json:"n,omitempty"`
	E      string   `json:"e,omitempty"`
	Crv    string   `json:"crv,omitempty"`
	X      string   `json:"x,omitempty"`
	Y      string   `json:"y,omitempty"`
	KeyOps []string `json:"key_ops,omitempty"`
}

type jwkSet struct {
	Keys []jwk `json:"keys"`
}

// publicJWK renders an asymmetric verification key as a JWK. HS256 and
// unknown key types report ok=false.
func publicJWK(k Key) (jwk, bool) {
	enc := base64.RawURLEncoding
	out := jwk{Kid: k.KID, Alg: string(k.Alg), Use: "sig"}
	switch pub := k.Key.(type) {
	case *rsa.PublicKey:
		out.Kty = "RSA"
		out.N = enc.EncodeToString(pub.N.Bytes())
		out.E = enc.EncodeToString(big.NewInt(int64(pub.E)).Bytes())
	case *ecdsa.PublicKey:
		// pub.Bytes() is the SEC1 uncompressed point 0x04 || X(32) || Y(32); slice out the two P-256 coordinates.
		b, err := pub.Bytes()
		if err != nil || len(b) != 65 {
			return jwk{}, false
		}
		out.Kty, out.Crv = "EC", "P-256"
		out.X = enc.EncodeToString(b[1:33])
		out.Y = enc.EncodeToString(b[33:65])
	case ed25519.PublicKey:
		out.Kty, out.Crv = "OKP", "Ed25519"
		out.X = enc.EncodeToString(pub)
	default:
		return jwk{}, false
	}
	return out, true
}

// JWKS returns an http.Handler serving the signer's asymmetric public keys
// as an RFC 7517 JWK set. HS256 keys are never published; an HS256-only
// signer serves an empty set. The body is rendered once at handler
// construction — signer keys do not change after NewSigner.
func (s *Signer) JWKS(opts ...ServeOption) http.Handler {
	var cfg serveConfig
	for _, o := range opts {
		o(&cfg)
	}
	set := jwkSet{Keys: []jwk{}}
	for _, k := range s.PublicKeys() {
		if j, ok := publicJWK(k); ok {
			set.Keys = append(set.Keys, j)
		}
	}
	body, err := json.Marshal(set)
	if err != nil {
		// Key material was validated at NewSigner; this is unreachable.
		panic(fmt.Sprintf("jwt: marshal jwks: %v", err))
	}
	cacheControl := ""
	if cfg.maxAge > 0 {
		cacheControl = fmt.Sprintf("public, max-age=%d", int(cfg.maxAge.Seconds()))
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if cacheControl != "" {
			w.Header().Set("Cache-Control", cacheControl)
		}
		_, _ = w.Write(body)
	})
}

// maxJWKSBody bounds the JWKS response size (DoS guard).
const maxJWKSBody = 1 << 20

// jwksCache is an in-memory JWK set with lazy fetch, TTL refresh, and
// unknown-kid refetch behind a cooldown. Failed refreshes serve the stale
// set; only a cold cache surfaces the fetch error.
type jwksCache struct {
	fetchedAt time.Time
	clk       clock.Clock

	client *http.Client
	keys   map[string]Key
	url    string

	group singleflight.Group[struct{}]

	refresh      time.Duration
	cooldown     time.Duration
	fetchTimeout time.Duration

	mu      sync.RWMutex
	fetched bool
}

// needFetch reports whether resolving kid requires a network fetch given the
// cache state at now: a cold cache, a set past its refresh TTL, or an unknown
// kid past the refetch cooldown.
func (c *jwksCache) needFetch(kid string, now time.Time) bool {
	c.mu.RLock()
	_, ok := c.keys[kid]
	fetched, fetchedAt := c.fetched, c.fetchedAt
	c.mu.RUnlock()

	if !fetched || now.Sub(fetchedAt) >= c.refresh {
		return true
	}
	return !ok && now.Sub(fetchedAt) >= c.cooldown
}

func (c *jwksCache) get(ctx context.Context, kid string) (Key, error) {
	c.mu.RLock()
	k, ok := c.keys[kid]
	fetched := c.fetched
	c.mu.RUnlock()

	need := c.needFetch(kid, c.clk.Now())
	if ok && !need {
		return k, nil
	}
	if need {
		_, _, err := c.group.Do(ctx, "fetch", func(ctx context.Context) (struct{}, error) {
			// Re-check under the flight: between a caller's staleness check and
			// its admission here, a just-completed flight may already have
			// refreshed the set — this caller then becomes a fresh leader and,
			// without the re-check, would repeat the network fetch it no longer
			// needs.
			if !c.needFetch(kid, c.clk.Now()) {
				return struct{}{}, nil
			}
			keys, err := c.fetchSet(ctx)
			if err != nil {
				return struct{}{}, err
			}
			c.mu.Lock()
			c.keys, c.fetchedAt, c.fetched = keys, c.clk.Now(), true
			c.mu.Unlock()
			return struct{}{}, nil
		})
		if err != nil && !fetched {
			return Key{}, fmt.Errorf("%w: jwks fetch: %w", ErrNoKeys, err)
		}
		// A failed refresh with a warm cache falls through to the stale set.
	}

	c.mu.RLock()
	defer c.mu.RUnlock()
	if len(c.keys) == 0 {
		return Key{}, fmt.Errorf("%w: jwks set is empty", ErrNoKeys)
	}
	if k, ok := c.keys[kid]; ok {
		return k, nil
	}
	return Key{}, fmt.Errorf("%w: kid %q", ErrUnknownKey, kid)
}

func (c *jwksCache) fetchSet(ctx context.Context) (map[string]Key, error) {
	// Fetches are deduplicated via singleflight, which runs this closure under
	// context.WithoutCancel — stripping the caller's deadline. Bound the shared
	// fetch here so a stalled JWKS endpoint cannot wedge every waiting Verify.
	if c.fetchTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.fetchTimeout)
		defer cancel()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	//nolint:nilaway // resp is non-nil whenever err is nil, per http.Client.Do's contract
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jwks endpoint returned status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxJWKSBody))
	if err != nil {
		return nil, err
	}
	var set jwkSet
	if err := json.Unmarshal(body, &set); err != nil {
		return nil, fmt.Errorf("jwks body: %w", err)
	}
	keys := make(map[string]Key, len(set.Keys))
	for _, j := range set.Keys {
		k, ok := parseJWK(j)
		if !ok {
			continue
		}
		keys[k.KID] = k
	}
	return keys, nil
}

// parseJWK converts a wire JWK into a verification Key. Unusable entries
// (wrong use/key_ops, unknown kty/crv, missing kid, undecodable material)
// report ok=false and are skipped, not fatal.
func parseJWK(j jwk) (Key, bool) {
	if j.Kid == "" {
		return Key{}, false // key resolution is kid-based; kid-less keys are unusable
	}
	if j.Use != "" && j.Use != "sig" {
		return Key{}, false
	}
	if len(j.KeyOps) > 0 && !slices.Contains(j.KeyOps, "verify") {
		return Key{}, false
	}
	enc := base64.RawURLEncoding
	switch j.Kty {
	case "RSA":
		if j.Alg != "" && j.Alg != string(RS256) {
			return Key{}, false
		}
		nb, err := enc.DecodeString(j.N)
		if err != nil {
			return Key{}, false
		}
		eb, err := enc.DecodeString(j.E)
		if err != nil || len(eb) == 0 || len(eb) > 4 {
			return Key{}, false
		}
		e := 0
		for _, b := range eb {
			e = e<<8 | int(b)
		}
		pub := &rsa.PublicKey{N: new(big.Int).SetBytes(nb), E: e}
		k := Key{KID: j.Kid, Alg: RS256, Key: pub}
		return k, checkVerifyKey(k) == nil
	case "EC":
		if j.Crv != "P-256" || (j.Alg != "" && j.Alg != string(ES256)) {
			return Key{}, false
		}
		xb, err := enc.DecodeString(j.X)
		if err != nil || len(xb) != 32 {
			return Key{}, false
		}
		yb, err := enc.DecodeString(j.Y)
		if err != nil || len(yb) != 32 {
			return Key{}, false
		}
		// SEC1 uncompressed point 0x04 || X(32) || Y(32); ParseUncompressedPublicKey validates on-curve + not-infinity.
		data := make([]byte, 1+len(xb)+len(yb))
		data[0] = 0x04
		copy(data[1:], xb)
		copy(data[1+len(xb):], yb)
		pub, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), data)
		if err != nil {
			return Key{}, false
		}
		return Key{KID: j.Kid, Alg: ES256, Key: pub}, true
	case "OKP":
		if j.Crv != "Ed25519" || (j.Alg != "" && j.Alg != string(EdDSA)) {
			return Key{}, false
		}
		xb, err := enc.DecodeString(j.X)
		if err != nil || len(xb) != ed25519.PublicKeySize {
			return Key{}, false
		}
		return Key{KID: j.Kid, Alg: EdDSA, Key: ed25519.PublicKey(xb)}, true
	default:
		return Key{}, false
	}
}
