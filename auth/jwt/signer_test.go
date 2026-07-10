package jwt_test

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/auth/jwt"
	"github.com/dmitrymomot/forge/crypto/keyset"
)

// --- helpers shared by later test files ---

func hsKeyset(t *testing.T) *keyset.Keyset {
	t.Helper()
	ks, err := keyset.New(
		keyset.WithPrimary(2, []byte("0123456789abcdef0123456789abcdef")),
		keyset.WithRetired(1, []byte("fedcba9876543210fedcba9876543210")),
	)
	if err != nil {
		t.Fatalf("keyset: %v", err)
	}
	return ks
}

func pkcs8(t *testing.T, key any) []byte {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("pkcs8: %v", err)
	}
	return der
}

func edKeyset(t *testing.T) (*keyset.Keyset, ed25519.PrivateKey) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519: %v", err)
	}
	ks, err := keyset.New(keyset.WithPrimary(1, pkcs8(t, priv)))
	if err != nil {
		t.Fatalf("keyset: %v", err)
	}
	return ks, priv
}

func ecKeyset(t *testing.T) (*keyset.Keyset, *ecdsa.PrivateKey) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa: %v", err)
	}
	ks, err := keyset.New(keyset.WithPrimary(1, pkcs8(t, priv)))
	if err != nil {
		t.Fatalf("keyset: %v", err)
	}
	return ks, priv
}

func rsaKeyset(t *testing.T) (*keyset.Keyset, *rsa.PrivateKey) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa: %v", err)
	}
	ks, err := keyset.New(keyset.WithPrimary(1, pkcs8(t, priv)))
	if err != nil {
		t.Fatalf("keyset: %v", err)
	}
	return ks, priv
}

// splitToken decodes the three segments of a compact JWS.
func splitToken(t *testing.T, tok string) (header map[string]any, payload, sig []byte, signingInput string) {
	t.Helper()
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		t.Fatalf("token has %d parts, want 3: %s", len(parts), tok)
	}
	hb, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatalf("header b64: %v", err)
	}
	if err := json.Unmarshal(hb, &header); err != nil {
		t.Fatalf("header json: %v", err)
	}
	payload, err = base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("payload b64: %v", err)
	}
	sig, err = base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatalf("sig b64: %v", err)
	}
	return header, payload, sig, parts[0] + "." + parts[1]
}

func testClaims() jwt.Claims {
	return jwt.Claims{
		Issuer:    "https://api.example.com",
		Subject:   "user-1",
		Audience:  jwt.Audience{"my-app"},
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}
}

// --- construction ---

func TestNewSignerKeySources(t *testing.T) {
	t.Parallel()

	t.Run("no key source", func(t *testing.T) {
		t.Parallel()
		if _, err := jwt.NewSigner(); !errors.Is(err, jwt.ErrBadKey) {
			t.Fatalf("got %v, want ErrBadKey", err)
		}
	})

	t.Run("two key sources", func(t *testing.T) {
		t.Parallel()
		ks, _ := edKeyset(t)
		_, err := jwt.NewSigner(jwt.WithKeyset(ks), jwt.WithHS256Keyset(hsKeyset(t)))
		if !errors.Is(err, jwt.ErrBadKey) {
			t.Fatalf("got %v, want ErrBadKey", err)
		}
	})

	t.Run("HS256 secret too short", func(t *testing.T) {
		t.Parallel()
		ks, err := keyset.New(keyset.WithPrimary(1, []byte("short")))
		if err != nil {
			t.Fatalf("keyset: %v", err)
		}
		if _, err := jwt.NewSigner(jwt.WithHS256Keyset(ks)); !errors.Is(err, jwt.ErrBadKey) {
			t.Fatalf("got %v, want ErrBadKey", err)
		}
	})

	t.Run("asymmetric keyset rejects raw bytes", func(t *testing.T) {
		t.Parallel()
		ks, err := keyset.New(keyset.WithPrimary(1, []byte("0123456789abcdef0123456789abcdef")))
		if err != nil {
			t.Fatalf("keyset: %v", err)
		}
		if _, err := jwt.NewSigner(jwt.WithKeyset(ks)); !errors.Is(err, jwt.ErrBadKey) {
			t.Fatalf("got %v, want ErrBadKey", err)
		}
	})

	t.Run("RSA under 2048 bits rejected", func(t *testing.T) {
		t.Parallel()
		priv, err := rsa.GenerateKey(rand.Reader, 1024)
		if err != nil {
			t.Fatalf("rsa: %v", err)
		}
		ks, err := keyset.New(keyset.WithPrimary(1, pkcs8(t, priv)))
		if err != nil {
			t.Fatalf("keyset: %v", err)
		}
		if _, err := jwt.NewSigner(jwt.WithKeyset(ks)); !errors.Is(err, jwt.ErrBadKey) {
			t.Fatalf("got %v, want ErrBadKey", err)
		}
	})

	t.Run("non-P256 curve rejected", func(t *testing.T) {
		t.Parallel()
		priv, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
		if err != nil {
			t.Fatalf("ecdsa: %v", err)
		}
		ks, err := keyset.New(keyset.WithPrimary(1, pkcs8(t, priv)))
		if err != nil {
			t.Fatalf("keyset: %v", err)
		}
		if _, err := jwt.NewSigner(jwt.WithKeyset(ks)); !errors.Is(err, jwt.ErrBadKey) {
			t.Fatalf("got %v, want ErrBadKey", err)
		}
	})

	t.Run("WithSignerKey rejects HS256", func(t *testing.T) {
		t.Parallel()
		_, priv, _ := ed25519.GenerateKey(rand.Reader)
		_, err := jwt.NewSigner(jwt.WithSignerKey("k1", jwt.HS256, priv))
		if !errors.Is(err, jwt.ErrBadKey) {
			t.Fatalf("got %v, want ErrBadKey", err)
		}
	})

	t.Run("WithSignerKey rejects alg/key mismatch", func(t *testing.T) {
		t.Parallel()
		_, priv, _ := ed25519.GenerateKey(rand.Reader)
		_, err := jwt.NewSigner(jwt.WithSignerKey("k1", jwt.RS256, priv))
		if !errors.Is(err, jwt.ErrBadKey) {
			t.Fatalf("got %v, want ErrBadKey", err)
		}
	})

	t.Run("WithSignerKey requires kid", func(t *testing.T) {
		t.Parallel()
		_, priv, _ := ed25519.GenerateKey(rand.Reader)
		_, err := jwt.NewSigner(jwt.WithSignerKey("", jwt.EdDSA, priv))
		if !errors.Is(err, jwt.ErrBadKey) {
			t.Fatalf("got %v, want ErrBadKey", err)
		}
	})
}

// --- signing, cross-checked with stdlib crypto ---

func TestSignHS256(t *testing.T) {
	t.Parallel()
	s, err := jwt.NewSigner(jwt.WithHS256Keyset(hsKeyset(t)))
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	tok, err := s.Sign(testClaims())
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	header, payload, sig, input := splitToken(t, tok)
	if header["alg"] != "HS256" || header["typ"] != "JWT" || header["kid"] != "2" {
		t.Fatalf("header %v: want alg=HS256 typ=JWT kid=2 (primary keyset version)", header)
	}
	var c jwt.Claims
	if err := json.Unmarshal(payload, &c); err != nil || c.Subject != "user-1" {
		t.Fatalf("payload %s: %v", payload, err)
	}
	m := hmac.New(sha256.New, []byte("0123456789abcdef0123456789abcdef"))
	m.Write([]byte(input))
	if !hmac.Equal(sig, m.Sum(nil)) {
		t.Fatal("HMAC does not verify with stdlib crypto")
	}
}

func TestSignEdDSA(t *testing.T) {
	t.Parallel()
	ks, priv := edKeyset(t)
	s, err := jwt.NewSigner(jwt.WithKeyset(ks))
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	tok, err := s.Sign(testClaims())
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	header, _, sig, input := splitToken(t, tok)
	if header["alg"] != "EdDSA" || header["kid"] != "1" {
		t.Fatalf("header %v", header)
	}
	if !ed25519.Verify(priv.Public().(ed25519.PublicKey), []byte(input), sig) {
		t.Fatal("Ed25519 signature does not verify with stdlib crypto")
	}
}

func TestSignES256RawSignature(t *testing.T) {
	t.Parallel()
	ks, priv := ecKeyset(t)
	s, err := jwt.NewSigner(jwt.WithKeyset(ks))
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	tok, err := s.Sign(testClaims())
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	header, _, sig, input := splitToken(t, tok)
	if header["alg"] != "ES256" {
		t.Fatalf("header %v", header)
	}
	if len(sig) != 64 {
		t.Fatalf("ES256 signature is %d bytes, want raw R||S 64", len(sig))
	}
	d := sha256.Sum256([]byte(input))
	r := new(big.Int).SetBytes(sig[:32])
	ss := new(big.Int).SetBytes(sig[32:])
	if !ecdsa.Verify(&priv.PublicKey, d[:], r, ss) {
		t.Fatal("ECDSA signature does not verify with stdlib crypto")
	}
}

func TestSignRS256(t *testing.T) {
	t.Parallel()
	ks, priv := rsaKeyset(t)
	s, err := jwt.NewSigner(jwt.WithKeyset(ks))
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	tok, err := s.Sign(testClaims())
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	header, _, sig, input := splitToken(t, tok)
	if header["alg"] != "RS256" {
		t.Fatalf("header %v", header)
	}
	d := sha256.Sum256([]byte(input))
	if err := rsa.VerifyPKCS1v15(&priv.PublicKey, crypto.SHA256, d[:], sig); err != nil {
		t.Fatalf("RSA signature does not verify with stdlib crypto: %v", err)
	}
}

func TestSignRejectsUnmarshalableClaims(t *testing.T) {
	t.Parallel()
	s, err := jwt.NewSigner(jwt.WithHS256Keyset(hsKeyset(t)))
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	if _, err := s.Sign(func() {}); err == nil {
		t.Fatal("want error for unmarshalable claims")
	}
}
