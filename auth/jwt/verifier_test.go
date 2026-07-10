package jwt_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/auth/jwt"
	"github.com/dmitrymomot/forge/crypto/keyset"
)

type accessClaims struct {
	jwt.Claims
	TenantID string `json:"tid"`
}

// hsToken hand-crafts an HS256 token so tests control every header field.
func hsToken(t *testing.T, secret []byte, headerJSON, payloadJSON string) string {
	t.Helper()
	enc := base64.RawURLEncoding
	input := enc.EncodeToString([]byte(headerJSON)) + "." + enc.EncodeToString([]byte(payloadJSON))
	m := hmac.New(sha256.New, secret)
	m.Write([]byte(input))
	return input + "." + enc.EncodeToString(m.Sum(nil))
}

func TestNewVerifierConstruction(t *testing.T) {
	t.Parallel()

	t.Run("requires a key source", func(t *testing.T) {
		t.Parallel()
		if _, err := jwt.NewVerifier(); !errors.Is(err, jwt.ErrBadKey) {
			t.Fatalf("got %v, want ErrBadKey", err)
		}
	})

	t.Run("duplicate kid rejected", func(t *testing.T) {
		t.Parallel()
		ks, _ := edKeyset(t)
		s, err := jwt.NewSigner(jwt.WithKeyset(ks))
		if err != nil {
			t.Fatalf("NewSigner: %v", err)
		}
		k := s.PublicKeys()[0]
		if _, err := jwt.NewVerifier(jwt.WithKeys(k, k)); !errors.Is(err, jwt.ErrBadKey) {
			t.Fatalf("got %v, want ErrBadKey", err)
		}
	})

	t.Run("short HS256 key in WithKeys rejected", func(t *testing.T) {
		t.Parallel()
		_, err := jwt.NewVerifier(jwt.WithKeys(jwt.Key{KID: "1", Alg: jwt.HS256, Key: []byte("short")}))
		if !errors.Is(err, jwt.ErrBadKey) {
			t.Fatalf("got %v, want ErrBadKey", err)
		}
	})

	t.Run("alg/key type mismatch rejected", func(t *testing.T) {
		t.Parallel()
		ks, _ := edKeyset(t)
		s, err := jwt.NewSigner(jwt.WithKeyset(ks))
		if err != nil {
			t.Fatalf("NewSigner: %v", err)
		}
		k := s.PublicKeys()[0]
		k.Alg = jwt.RS256 // Ed25519 key claiming RS256
		if _, err := jwt.NewVerifier(jwt.WithKeys(k)); !errors.Is(err, jwt.ErrBadKey) {
			t.Fatalf("got %v, want ErrBadKey", err)
		}
	})
}

func TestRoundTripAllAlgs(t *testing.T) {
	t.Parallel()
	cases := map[string]func(*testing.T) (*jwt.Signer, *jwt.Verifier){
		"HS256": func(t *testing.T) (*jwt.Signer, *jwt.Verifier) {
			ks := hsKeyset(t)
			s, err := jwt.NewSigner(jwt.WithHS256Keyset(ks))
			if err != nil {
				t.Fatalf("NewSigner: %v", err)
			}
			v, err := jwt.NewVerifier(jwt.WithVerifyHS256Keyset(ks))
			if err != nil {
				t.Fatalf("NewVerifier: %v", err)
			}
			return s, v
		},
		"EdDSA": func(t *testing.T) (*jwt.Signer, *jwt.Verifier) {
			ks, _ := edKeyset(t)
			return signerVerifierPair(t, ks)
		},
		"ES256": func(t *testing.T) (*jwt.Signer, *jwt.Verifier) {
			ks, _ := ecKeyset(t)
			return signerVerifierPair(t, ks)
		},
		"RS256": func(t *testing.T) (*jwt.Signer, *jwt.Verifier) {
			ks, _ := rsaKeyset(t)
			return signerVerifierPair(t, ks)
		},
	}
	for name, mk := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s, v := mk(t)
			in := accessClaims{Claims: testClaims(), TenantID: "t-42"}
			tok, err := s.Sign(in)
			if err != nil {
				t.Fatalf("Sign: %v", err)
			}
			out, err := jwt.Verify[accessClaims](t.Context(), v, tok)
			if err != nil {
				t.Fatalf("Verify: %v", err)
			}
			if out.Subject != "user-1" || out.TenantID != "t-42" {
				t.Fatalf("claims %+v", out)
			}
		})
	}
}

func signerVerifierPair(t *testing.T, ks *keyset.Keyset) (*jwt.Signer, *jwt.Verifier) {
	t.Helper()
	s, err := jwt.NewSigner(jwt.WithKeyset(ks))
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	v, err := jwt.NewVerifier(jwt.WithKeys(s.PublicKeys()...))
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	return s, v
}

// Retired keyset versions must keep verifying (rotation).
func TestVerifyRetiredKeysetVersion(t *testing.T) {
	t.Parallel()
	oldKS, err := keyset.New(keyset.WithPrimary(1, []byte("fedcba9876543210fedcba9876543210")))
	if err != nil {
		t.Fatalf("keyset: %v", err)
	}
	oldSigner, err := jwt.NewSigner(jwt.WithHS256Keyset(oldKS))
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	tok, err := oldSigner.Sign(testClaims())
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	// hsKeyset has primary 2 and retired 1 (same material as oldKS's primary).
	v, err := jwt.NewVerifier(jwt.WithVerifyHS256Keyset(hsKeyset(t)))
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	if _, err := jwt.Verify[jwt.Claims](t.Context(), v, tok); err != nil {
		t.Fatalf("retired version does not verify: %v", err)
	}
}

// WithVerifyKeyset derives verification (public) keys for every version in
// an asymmetric keyset. This exercises both the primary version (kid "2")
// and a retired version (kid "1") to prove the retired public half was
// derived correctly, not just the primary.
func TestVerifyWithVerifyKeyset(t *testing.T) {
	t.Parallel()

	priv1, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa priv1: %v", err)
	}
	priv2, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa priv2: %v", err)
	}

	ks, err := keyset.New(
		keyset.WithPrimary(2, pkcs8(t, priv2)),
		keyset.WithRetired(1, pkcs8(t, priv1)),
	)
	if err != nil {
		t.Fatalf("keyset: %v", err)
	}

	v, err := jwt.NewVerifier(jwt.WithVerifyKeyset(ks))
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	t.Run("primary version", func(t *testing.T) {
		t.Parallel()
		s2, err := jwt.NewSigner(jwt.WithKeyset(ks))
		if err != nil {
			t.Fatalf("NewSigner: %v", err)
		}
		tok, err := s2.Sign(testClaims())
		if err != nil {
			t.Fatalf("Sign: %v", err)
		}
		out, err := jwt.Verify[jwt.Claims](t.Context(), v, tok)
		if err != nil {
			t.Fatalf("Verify primary: %v", err)
		}
		if out.Subject != "user-1" {
			t.Fatalf("claims %+v, want Subject user-1", out)
		}
	})

	t.Run("retired version", func(t *testing.T) {
		t.Parallel()
		s1, err := jwt.NewSigner(jwt.WithSignerKey("1", jwt.ES256, priv1))
		if err != nil {
			t.Fatalf("NewSigner: %v", err)
		}
		tok, err := s1.Sign(testClaims())
		if err != nil {
			t.Fatalf("Sign: %v", err)
		}
		out, err := jwt.Verify[jwt.Claims](t.Context(), v, tok)
		if err != nil {
			t.Fatalf("Verify retired: %v", err)
		}
		if out.Subject != "user-1" {
			t.Fatalf("claims %+v, want Subject user-1", out)
		}
	})
}

func TestVerifyMalformed(t *testing.T) {
	t.Parallel()
	v, err := jwt.NewVerifier(jwt.WithVerifyHS256Keyset(hsKeyset(t)))
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	s, err := jwt.NewSigner(jwt.WithHS256Keyset(hsKeyset(t)))
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	good, err := s.Sign(testClaims())
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	parts := strings.Split(good, ".")
	cases := map[string]string{
		"empty":              "",
		"garbage":            "not-a-token",
		"two segments":       parts[0] + "." + parts[1],
		"four segments":      good + ".extra",
		"padded base64":      parts[0] + "==." + parts[1] + "." + parts[2],
		"standard base64":    strings.ReplaceAll(good, "_", "/") + "+",
		"non-object header":  hsToken(t, []byte("0123456789abcdef0123456789abcdef"), `"HS256"`, `{}`),
		"non-object payload": hsToken(t, []byte("0123456789abcdef0123456789abcdef"), `{"alg":"HS256","kid":"2"}`, `[1,2]`),
		"bad typ":            hsToken(t, []byte("0123456789abcdef0123456789abcdef"), `{"alg":"HS256","kid":"2","typ":"JWE"}`, `{}`),
		"oversized":          good + strings.Repeat("a", 64<<10),
	}
	for name, tok := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := jwt.Verify[jwt.Claims](t.Context(), v, tok); !errors.Is(err, jwt.ErrMalformed) {
				t.Fatalf("got %v, want ErrMalformed", err)
			}
		})
	}
}

func TestVerifyKeyResolution(t *testing.T) {
	t.Parallel()
	secret := []byte("0123456789abcdef0123456789abcdef")
	exp := time.Now().Add(time.Hour).Unix()
	payload := `{"exp":` + itoa(exp) + `}`

	t.Run("unknown kid", func(t *testing.T) {
		t.Parallel()
		v, err := jwt.NewVerifier(jwt.WithVerifyHS256Keyset(hsKeyset(t)))
		if err != nil {
			t.Fatalf("NewVerifier: %v", err)
		}
		tok := hsToken(t, secret, `{"alg":"HS256","kid":"99"}`, payload)
		if _, err := jwt.Verify[jwt.Claims](t.Context(), v, tok); !errors.Is(err, jwt.ErrUnknownKey) {
			t.Fatalf("got %v, want ErrUnknownKey", err)
		}
	})

	t.Run("no kid with single key uses it", func(t *testing.T) {
		t.Parallel()
		v, err := jwt.NewVerifier(jwt.WithKeys(jwt.Key{KID: "only", Alg: jwt.HS256, Key: secret}))
		if err != nil {
			t.Fatalf("NewVerifier: %v", err)
		}
		tok := hsToken(t, secret, `{"alg":"HS256"}`, payload)
		if _, err := jwt.Verify[jwt.Claims](t.Context(), v, tok); err != nil {
			t.Fatalf("single-key rule failed: %v", err)
		}
	})

	t.Run("no kid with multiple keys rejected", func(t *testing.T) {
		t.Parallel()
		v, err := jwt.NewVerifier(jwt.WithVerifyHS256Keyset(hsKeyset(t))) // 2 versions
		if err != nil {
			t.Fatalf("NewVerifier: %v", err)
		}
		tok := hsToken(t, secret, `{"alg":"HS256"}`, payload)
		if _, err := jwt.Verify[jwt.Claims](t.Context(), v, tok); !errors.Is(err, jwt.ErrUnknownKey) {
			t.Fatalf("got %v, want ErrUnknownKey", err)
		}
	})
}

func TestVerifyAlgAttacks(t *testing.T) {
	t.Parallel()
	secret := []byte("0123456789abcdef0123456789abcdef")
	exp := time.Now().Add(time.Hour).Unix()
	payload := `{"exp":` + itoa(exp) + `}`

	t.Run("alg none", func(t *testing.T) {
		t.Parallel()
		v, err := jwt.NewVerifier(jwt.WithKeys(jwt.Key{KID: "only", Alg: jwt.HS256, Key: secret}))
		if err != nil {
			t.Fatalf("NewVerifier: %v", err)
		}
		enc := base64.RawURLEncoding
		tok := enc.EncodeToString([]byte(`{"alg":"none"}`)) + "." + enc.EncodeToString([]byte(payload)) + "."
		if _, err := jwt.Verify[jwt.Claims](t.Context(), v, tok); !errors.Is(err, jwt.ErrSignature) && !errors.Is(err, jwt.ErrMalformed) {
			t.Fatalf("got %v, want ErrSignature or ErrMalformed", err)
		}
	})

	t.Run("HS256 signed with the public key bytes (confusion)", func(t *testing.T) {
		t.Parallel()
		ks, priv := ecKeyset(t)
		s, err := jwt.NewSigner(jwt.WithKeyset(ks))
		if err != nil {
			t.Fatalf("NewSigner: %v", err)
		}
		v, err := jwt.NewVerifier(jwt.WithKeys(s.PublicKeys()...))
		if err != nil {
			t.Fatalf("NewVerifier: %v", err)
		}
		// Attacker HMACs with the DER of the public key and claims HS256 for kid "1".
		pubDER, err := ecdsaPublicDER(&priv.PublicKey)
		if err != nil {
			t.Fatalf("marshal public: %v", err)
		}
		tok := hsToken(t, pubDER, `{"alg":"HS256","kid":"1"}`, payload)
		if _, err := jwt.Verify[jwt.Claims](t.Context(), v, tok); !errors.Is(err, jwt.ErrSignature) {
			t.Fatalf("confusion attack not rejected: %v", err)
		}
	})

	t.Run("tampered payload", func(t *testing.T) {
		t.Parallel()
		s, err := jwt.NewSigner(jwt.WithHS256Keyset(hsKeyset(t)))
		if err != nil {
			t.Fatalf("NewSigner: %v", err)
		}
		v, err := jwt.NewVerifier(jwt.WithVerifyHS256Keyset(hsKeyset(t)))
		if err != nil {
			t.Fatalf("NewVerifier: %v", err)
		}
		tok, err := s.Sign(testClaims())
		if err != nil {
			t.Fatalf("Sign: %v", err)
		}
		parts := strings.Split(tok, ".")
		forged := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"admin","exp":` + itoa(exp) + `}`))
		if _, err := jwt.Verify[jwt.Claims](t.Context(), v, parts[0]+"."+forged+"."+parts[2]); !errors.Is(err, jwt.ErrSignature) {
			t.Fatalf("got %v, want ErrSignature", err)
		}
	})
}

func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}

func ecdsaPublicDER(pub *ecdsa.PublicKey) ([]byte, error) {
	return x509.MarshalPKIXPublicKey(pub)
}
