package jwt_test

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/auth/jwt"
	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/crypto/keyset"
)

// jwksServer serves the JWKS of the given signer and counts fetches.
type jwksServer struct {
	*httptest.Server
	hits atomic.Int64
	mu   sync.Mutex
	h    http.Handler
	fail bool
}

func newJWKSServer(t *testing.T, s *jwt.Signer) *jwksServer {
	t.Helper()
	js := &jwksServer{h: s.JWKS()}
	js.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		js.hits.Add(1)
		js.mu.Lock()
		fail, h := js.fail, js.h
		js.mu.Unlock()
		if fail {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(js.Close)
	return js
}

func (js *jwksServer) swap(h http.Handler) { js.mu.Lock(); js.h = h; js.mu.Unlock() }
func (js *jwksServer) setFail(f bool)      { js.mu.Lock(); js.fail = f; js.mu.Unlock() }

func TestJWKSFetchVerify(t *testing.T) {
	t.Parallel()
	ks, _ := edKeyset(t)
	s, err := jwt.NewSigner(jwt.WithKeyset(ks))
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	js := newJWKSServer(t, s)
	v, err := jwt.NewVerifier(jwt.WithJWKSURL(js.URL))
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	tok, err := s.Sign(testClaims())
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if js.hits.Load() != 0 {
		t.Fatal("fetch happened before first Verify (must be lazy)")
	}
	got, err := jwt.Verify[jwt.Claims](t.Context(), v, tok)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got.Subject != "user-1" {
		t.Fatalf("claims %+v", got)
	}
	// Second verify is served from cache.
	if _, err := jwt.Verify[jwt.Claims](t.Context(), v, tok); err != nil {
		t.Fatalf("cached Verify: %v", err)
	}
	if hits := js.hits.Load(); hits != 1 {
		t.Fatalf("got %d fetches, want 1 (cache miss only on first Verify)", hits)
	}
}

func TestJWKSRotationPickupAndCooldown(t *testing.T) {
	t.Parallel()
	ks1, _ := edKeyset(t)
	s1, err := jwt.NewSigner(jwt.WithKeyset(ks1))
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	js := newJWKSServer(t, s1)
	mock := clock.NewMock(time.Unix(1_700_000_000, 0))
	v, err := jwt.NewVerifier(jwt.WithJWKSURL(js.URL), jwt.WithClock(mock))
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	tok1, err := s1.Sign(freshClaims(mock.Now()))
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if _, err := jwt.Verify[jwt.Claims](t.Context(), v, tok1); err != nil {
		t.Fatalf("initial Verify: %v", err)
	}

	// Rotate: new signer under kid "2" (WithSignerKey to control the kid).
	_, priv2, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519: %v", err)
	}
	s2, err := jwt.NewSigner(jwt.WithSignerKey("2", jwt.EdDSA, priv2))
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	js.swap(s2.JWKS())
	tok2, err := s2.Sign(freshClaims(mock.Now()))
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	// Within the cooldown (default 1m) the unknown kid must NOT refetch.
	if _, err := jwt.Verify[jwt.Claims](t.Context(), v, tok2); !errors.Is(err, jwt.ErrUnknownKey) {
		t.Fatalf("got %v, want ErrUnknownKey inside cooldown", err)
	}
	if hits := js.hits.Load(); hits != 1 {
		t.Fatalf("refetched inside cooldown: %d hits", hits)
	}

	// Past the cooldown the unknown kid triggers a refetch and verifies.
	mock.Advance(2 * time.Minute)
	if _, err := jwt.Verify[jwt.Claims](t.Context(), v, tok2); err != nil {
		t.Fatalf("rotation pickup: %v", err)
	}
	if hits := js.hits.Load(); hits != 2 {
		t.Fatalf("got %d hits, want 2", hits)
	}
}

func TestJWKSStaleIfErrorAndColdError(t *testing.T) {
	t.Parallel()
	ks, _ := edKeyset(t)
	s, err := jwt.NewSigner(jwt.WithKeyset(ks))
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}

	t.Run("cold cache surfaces fetch error", func(t *testing.T) {
		t.Parallel()
		js := newJWKSServer(t, s)
		js.setFail(true)
		v, err := jwt.NewVerifier(jwt.WithJWKSURL(js.URL))
		if err != nil {
			t.Fatalf("NewVerifier: %v", err)
		}
		tok, err := s.Sign(testClaims())
		if err != nil {
			t.Fatalf("Sign: %v", err)
		}
		if _, err := jwt.Verify[jwt.Claims](t.Context(), v, tok); !errors.Is(err, jwt.ErrNoKeys) {
			t.Fatalf("got %v, want ErrNoKeys on cold fetch failure", err)
		}
	})

	t.Run("stale keys served when refresh fails", func(t *testing.T) {
		t.Parallel()
		js := newJWKSServer(t, s)
		mock := clock.NewMock(time.Unix(1_700_000_000, 0))
		v, err := jwt.NewVerifier(jwt.WithJWKSURL(js.URL), jwt.WithClock(mock))
		if err != nil {
			t.Fatalf("NewVerifier: %v", err)
		}
		tok, err := s.Sign(freshClaims(mock.Now()))
		if err != nil {
			t.Fatalf("Sign: %v", err)
		}
		if _, err := jwt.Verify[jwt.Claims](t.Context(), v, tok); err != nil {
			t.Fatalf("warm up: %v", err)
		}
		js.setFail(true)
		mock.Advance(2 * time.Hour) // past the 1h refresh TTL
		if _, err := jwt.Verify[jwt.Claims](t.Context(), v, tok); err != nil {
			t.Fatalf("stale-if-error not honored: %v", err)
		}
	})
}

func TestJWKSConcurrentVerifySingleFetch(t *testing.T) {
	t.Parallel()
	ks, _ := edKeyset(t)
	s, err := jwt.NewSigner(jwt.WithKeyset(ks))
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	js := newJWKSServer(t, s)
	v, err := jwt.NewVerifier(jwt.WithJWKSURL(js.URL))
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	tok, err := s.Sign(testClaims())
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			if _, err := jwt.Verify[jwt.Claims](t.Context(), v, tok); err != nil {
				t.Errorf("concurrent Verify: %v", err)
			}
		})
	}
	wg.Wait()
	if hits := js.hits.Load(); hits != 1 {
		t.Fatalf("got %d fetches for 50 concurrent verifies, want 1", hits)
	}
}

func TestJWKSSkipsUnusableKeys(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// enc-use key, unknown kty, and kid-less key must all be skipped -> empty set.
		_, _ = w.Write([]byte(`{"keys":[
			{"kty":"RSA","kid":"enc","use":"enc","n":"AQAB","e":"AQAB"},
			{"kty":"WEIRD","kid":"w"},
			{"kty":"OKP","crv":"Ed25519","x":"11qYAYKxCrfVS_7TyWQHOg7hcvPapiMlrwIaaPcHURo"}
		]}`))
	}))
	t.Cleanup(srv.Close)
	v, err := jwt.NewVerifier(jwt.WithJWKSURL(srv.URL))
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	ks, _ := edKeyset(t)
	s, err := jwt.NewSigner(jwt.WithKeyset(ks))
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	tok, err := s.Sign(testClaims())
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if _, err := jwt.Verify[jwt.Claims](t.Context(), v, tok); !errors.Is(err, jwt.ErrNoKeys) {
		t.Fatalf("got %v, want ErrNoKeys when all JWKS entries are unusable", err)
	}
}

// TestJWKSFetchVerifyRSAAndEC proves the RSA and EC fetch -> parseJWK ->
// verify round-trips work end to end, including the modern
// ecdsa.ParseUncompressedPublicKey-based EC branch of parseJWK.
func TestJWKSFetchVerifyRSAAndEC(t *testing.T) {
	t.Parallel()

	t.Run("RSA", func(t *testing.T) {
		t.Parallel()
		ks, _ := rsaKeyset(t)
		s, err := jwt.NewSigner(jwt.WithKeyset(ks))
		if err != nil {
			t.Fatalf("NewSigner: %v", err)
		}
		js := newJWKSServer(t, s)
		v, err := jwt.NewVerifier(jwt.WithJWKSURL(js.URL))
		if err != nil {
			t.Fatalf("NewVerifier: %v", err)
		}
		tok, err := s.Sign(testClaims())
		if err != nil {
			t.Fatalf("Sign: %v", err)
		}
		got, err := jwt.Verify[jwt.Claims](t.Context(), v, tok)
		if err != nil {
			t.Fatalf("Verify: %v", err)
		}
		if got.Subject != "user-1" {
			t.Fatalf("claims %+v", got)
		}
	})

	t.Run("EC", func(t *testing.T) {
		t.Parallel()
		ks, _ := ecKeyset(t)
		s, err := jwt.NewSigner(jwt.WithKeyset(ks))
		if err != nil {
			t.Fatalf("NewSigner: %v", err)
		}
		js := newJWKSServer(t, s)
		v, err := jwt.NewVerifier(jwt.WithJWKSURL(js.URL))
		if err != nil {
			t.Fatalf("NewVerifier: %v", err)
		}
		tok, err := s.Sign(testClaims())
		if err != nil {
			t.Fatalf("Sign: %v", err)
		}
		got, err := jwt.Verify[jwt.Claims](t.Context(), v, tok)
		if err != nil {
			t.Fatalf("Verify: %v", err)
		}
		if got.Subject != "user-1" {
			t.Fatalf("claims %+v", got)
		}
	})
}

// TestJWKSFetchTimeoutBounded proves a stalled JWKS endpoint cannot wedge
// Verify forever: singleflight runs the fetch under context.WithoutCancel,
// stripping the caller's deadline, so WithFetchTimeout must bound the fetch
// on its own.
func TestJWKSFetchTimeoutBounded(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)

	ks, _ := edKeyset(t)
	s, err := jwt.NewSigner(jwt.WithKeyset(ks))
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	tok, err := s.Sign(testClaims())
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	v, err := jwt.NewVerifier(jwt.WithJWKSURL(srv.URL, jwt.WithFetchTimeout(200*time.Millisecond)))
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	start := time.Now()
	_, err = jwt.Verify[jwt.Claims](t.Context(), v, tok)
	elapsed := time.Since(start)
	if !errors.Is(err, jwt.ErrNoKeys) {
		t.Fatalf("got %v, want ErrNoKeys on timed-out fetch", err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("Verify took %s, want well under 2s (fetch timeout not bounding the block)", elapsed)
	}
}

// freshClaims uses a 48h exp: the stale-if-error test advances the mock
// clock 2h past the refresh TTL and the token must still be unexpired.
func freshClaims(now time.Time) jwt.Claims {
	return jwt.Claims{Subject: "user-1", ExpiresAt: jwt.NewNumericDate(now.Add(48 * time.Hour))}
}

// TestVerifierCombinedKeySources proves the four verifier key sources merge
// coherently into one Verifier: WithKeys, WithVerifyHS256Keyset,
// WithVerifyKeyset, and WithJWKSURL each resolve their own signer's token
// through their own path, provided their kids are disjoint.
func TestVerifierCombinedKeySources(t *testing.T) {
	t.Parallel()

	// Source A: WithKeys — an explicit Ed25519 signer, kid "kid-a".
	_, privA, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519: %v", err)
	}
	sA, err := jwt.NewSigner(jwt.WithSignerKey("kid-a", jwt.EdDSA, privA))
	if err != nil {
		t.Fatalf("NewSigner sA: %v", err)
	}

	// Source B: WithVerifyHS256Keyset — HS256 keyset version 5, kid "5".
	hsKS, err := keyset.New(keyset.WithPrimary(5, []byte("0123456789abcdef0123456789abcdef")))
	if err != nil {
		t.Fatalf("keyset hsKS: %v", err)
	}
	sB, err := jwt.NewSigner(jwt.WithHS256Keyset(hsKS))
	if err != nil {
		t.Fatalf("NewSigner sB: %v", err)
	}

	// Source C: WithVerifyKeyset — EC keyset version 7, kid "7".
	ecPriv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa ecPriv: %v", err)
	}
	ecKS, err := keyset.New(keyset.WithPrimary(7, pkcs8(t, ecPriv)))
	if err != nil {
		t.Fatalf("keyset ecKS: %v", err)
	}
	sC, err := jwt.NewSigner(jwt.WithKeyset(ecKS))
	if err != nil {
		t.Fatalf("NewSigner sC: %v", err)
	}

	// Source D: WithJWKSURL — an ES256 signer served over JWKS, kid "kid-d".
	ecPrivD, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa ecPrivD: %v", err)
	}
	sD, err := jwt.NewSigner(jwt.WithSignerKey("kid-d", jwt.ES256, ecPrivD))
	if err != nil {
		t.Fatalf("NewSigner sD: %v", err)
	}
	js := newJWKSServer(t, sD)

	v, err := jwt.NewVerifier(
		jwt.WithKeys(sA.PublicKeys()...),
		jwt.WithVerifyHS256Keyset(hsKS),
		jwt.WithVerifyKeyset(ecKS),
		jwt.WithJWKSURL(js.URL),
	)
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	for name, s := range map[string]*jwt.Signer{"A-WithKeys": sA, "B-HS256Keyset": sB, "C-Keyset": sC, "D-JWKS": sD} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			tok, err := s.Sign(testClaims())
			if err != nil {
				t.Fatalf("Sign: %v", err)
			}
			got, err := jwt.Verify[jwt.Claims](t.Context(), v, tok)
			if err != nil {
				t.Fatalf("Verify: %v", err)
			}
			if got.Subject != "user-1" {
				t.Fatalf("claims %+v, want Subject user-1", got)
			}
		})
	}
}

// TestVerifyNoKidWithJWKSSourceRejected guards resolveKey's no-kid rule:
// the sole-static-key fallback fires only when the verifier holds exactly
// one static key AND has no JWKS source (v.jwks == nil). Here the verifier
// has zero static keys and a JWKS source, so a kid-less header must be
// rejected outright rather than silently trying a key or triggering a
// try-all-keys scan.
func TestVerifyNoKidWithJWKSSourceRejected(t *testing.T) {
	t.Parallel()
	ks, _ := edKeyset(t)
	s, err := jwt.NewSigner(jwt.WithKeyset(ks))
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	js := newJWKSServer(t, s) // serves exactly one key

	v, err := jwt.NewVerifier(jwt.WithJWKSURL(js.URL))
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	enc := base64.RawURLEncoding
	header := `{"alg":"EdDSA"}` // no kid
	payload := `{"exp":` + strconv.FormatInt(time.Now().Add(24*time.Hour).Unix(), 10) + `}`
	tok := enc.EncodeToString([]byte(header)) + "." + enc.EncodeToString([]byte(payload)) + "." + enc.EncodeToString([]byte("sig"))

	if _, err := jwt.Verify[jwt.Claims](t.Context(), v, tok); !errors.Is(err, jwt.ErrUnknownKey) {
		t.Fatalf("got %v, want ErrUnknownKey", err)
	}
}
