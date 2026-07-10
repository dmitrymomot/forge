package jwt_test

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/auth/jwt"
)

type jwksDoc struct {
	Keys []map[string]any `json:"keys"`
}

func fetchJWKS(t *testing.T, s *jwt.Signer, opts ...jwt.ServeOption) (*httptest.ResponseRecorder, jwksDoc) {
	t.Helper()
	rec := httptest.NewRecorder()
	s.JWKS(opts...).ServeHTTP(rec, httptest.NewRequest("GET", "/.well-known/jwks.json", nil))
	var doc jwksDoc
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("jwks body %s: %v", rec.Body.Bytes(), err)
	}
	return rec, doc
}

func TestPublicKeysExcludesHS256(t *testing.T) {
	t.Parallel()
	s, err := jwt.NewSigner(jwt.WithHS256Keyset(hsKeyset(t)))
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	if keys := s.PublicKeys(); len(keys) != 0 {
		t.Fatalf("HS256 secrets leaked via PublicKeys: %v", keys)
	}
}

func TestPublicKeysEdDSA(t *testing.T) {
	t.Parallel()
	ks, priv := edKeyset(t)
	s, err := jwt.NewSigner(jwt.WithKeyset(ks))
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	keys := s.PublicKeys()
	if len(keys) != 1 || keys[0].KID != "1" || keys[0].Alg != jwt.EdDSA {
		t.Fatalf("got %+v", keys)
	}
	pub, ok := keys[0].Key.(ed25519.PublicKey)
	if !ok || !pub.Equal(priv.Public().(ed25519.PublicKey)) {
		t.Fatalf("wrong public key: %T", keys[0].Key)
	}
}

func TestJWKSServeEdDSA(t *testing.T) {
	t.Parallel()
	ks, priv := edKeyset(t)
	s, err := jwt.NewSigner(jwt.WithKeyset(ks))
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	rec, doc := fetchJWKS(t, s)
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content type %q", ct)
	}
	if len(doc.Keys) != 1 {
		t.Fatalf("got %d keys", len(doc.Keys))
	}
	k := doc.Keys[0]
	if k["kty"] != "OKP" || k["crv"] != "Ed25519" || k["kid"] != "1" || k["alg"] != "EdDSA" || k["use"] != "sig" {
		t.Fatalf("jwk %v", k)
	}
	x, err := base64.RawURLEncoding.DecodeString(k["x"].(string))
	if err != nil || !ed25519.PublicKey(x).Equal(priv.Public().(ed25519.PublicKey)) {
		t.Fatalf("x mismatch: %v", err)
	}
}

func TestJWKSServeAllKtys(t *testing.T) {
	t.Parallel()
	for name, mk := range map[string]func(*testing.T) *jwt.Signer{
		"RSA": func(t *testing.T) *jwt.Signer {
			ks, _ := rsaKeyset(t)
			s, err := jwt.NewSigner(jwt.WithKeyset(ks))
			if err != nil {
				t.Fatalf("NewSigner: %v", err)
			}
			return s
		},
		"EC": func(t *testing.T) *jwt.Signer {
			ks, _ := ecKeyset(t)
			s, err := jwt.NewSigner(jwt.WithKeyset(ks))
			if err != nil {
				t.Fatalf("NewSigner: %v", err)
			}
			return s
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := mk(t)
			_, doc := fetchJWKS(t, s)
			if len(doc.Keys) != 1 || doc.Keys[0]["kty"] != name {
				t.Fatalf("got %v, want kty %s", doc.Keys, name)
			}
			if name == "EC" && doc.Keys[0]["crv"] != "P-256" {
				t.Fatalf("crv %v", doc.Keys[0]["crv"])
			}

			switch name {
			case "RSA":
				nb, err := base64.RawURLEncoding.DecodeString(doc.Keys[0]["n"].(string))
				if err != nil {
					t.Fatalf("decode n: %v", err)
				}
				eb, err := base64.RawURLEncoding.DecodeString(doc.Keys[0]["e"].(string))
				if err != nil {
					t.Fatalf("decode e: %v", err)
				}
				e := 0
				for _, b := range eb {
					e = e<<8 | int(b)
				}
				got := &rsa.PublicKey{N: new(big.Int).SetBytes(nb), E: e}
				want, ok := s.PublicKeys()[0].Key.(*rsa.PublicKey)
				if !ok || !got.Equal(want) {
					t.Fatalf("RSA public key mismatch: got %+v, want %+v", got, want)
				}
			case "EC":
				xb, err := base64.RawURLEncoding.DecodeString(doc.Keys[0]["x"].(string))
				if err != nil || len(xb) != 32 {
					t.Fatalf("decode x: len=%d err=%v", len(xb), err)
				}
				yb, err := base64.RawURLEncoding.DecodeString(doc.Keys[0]["y"].(string))
				if err != nil || len(yb) != 32 {
					t.Fatalf("decode y: len=%d err=%v", len(yb), err)
				}
				data := append([]byte{0x04}, append(xb, yb...)...)
				got, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), data)
				if err != nil {
					t.Fatalf("ParseUncompressedPublicKey: %v", err)
				}
				want, ok := s.PublicKeys()[0].Key.(*ecdsa.PublicKey)
				if !ok || !got.Equal(want) {
					t.Fatalf("EC public key mismatch: got %+v, want %+v", got, want)
				}
			}
		})
	}
}

func TestJWKSServeHS256Empty(t *testing.T) {
	t.Parallel()
	s, err := jwt.NewSigner(jwt.WithHS256Keyset(hsKeyset(t)))
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	rec, doc := fetchJWKS(t, s)
	if len(doc.Keys) != 0 {
		t.Fatalf("HS256 keys served: %v", doc.Keys)
	}
	if rec.Body.String() != `{"keys":[]}` {
		t.Fatalf("body %q, want {\"keys\":[]}", rec.Body.String())
	}
}

func TestJWKSServeCacheControl(t *testing.T) {
	t.Parallel()
	ks, _ := edKeyset(t)
	s, err := jwt.NewSigner(jwt.WithKeyset(ks))
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	rec, _ := fetchJWKS(t, s, jwt.WithCacheControl(5*time.Minute))
	if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=300" {
		t.Fatalf("Cache-Control %q", cc)
	}
	rec, _ = fetchJWKS(t, s)
	if cc := rec.Header().Get("Cache-Control"); cc != "" {
		t.Fatalf("unexpected Cache-Control %q", cc)
	}
}
