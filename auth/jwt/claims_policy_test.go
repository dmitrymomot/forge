package jwt_test

import (
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/auth/jwt"
	"github.com/dmitrymomot/forge/core/clock"
)

// RFC 7515 Appendix A.1 golden vector (HS256, no kid header).
// If ONLY this test fails while round-trip tests pass, re-copy these two
// constants from the RFC text before debugging the implementation.
const (
	rfc7515A1Token = "eyJ0eXAiOiJKV1QiLA0KICJhbGciOiJIUzI1NiJ9.eyJpc3MiOiJqb2UiLA0KICJleHAiOjEzMDA4MTkzODAsDQogImh0dHA6Ly9leGFtcGxlLmNvbS9pc19yb290Ijp0cnVlfQ.dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	rfc7515A1Key   = "AyM1SysPpbyDfgZld3umj1qzKObwVMkoqQ-EstJQLr_T-1qS0gZH75aKtMN3Yj0iPS4hcgUuTwjAzZr1Z9CAow"
)

func TestRFC7515A1GoldenVector(t *testing.T) {
	t.Parallel()
	secret, err := base64.RawURLEncoding.DecodeString(rfc7515A1Key)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	// exp is 1300819380 (2011) — pin the clock before it.
	v, err := jwt.NewVerifier(
		jwt.WithKeys(jwt.Key{KID: "a1", Alg: jwt.HS256, Key: secret}),
		jwt.WithClock(clock.NewMock(time.Unix(1300819000, 0))),
		jwt.WithIssuer("joe"),
	)
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	type rootClaims struct {
		jwt.Claims
		IsRoot bool `json:"http://example.com/is_root"`
	}
	got, err := jwt.Verify[rootClaims](t.Context(), v, rfc7515A1Token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got.Issuer != "joe" || !got.IsRoot || got.ExpiresAt.Unix() != 1300819380 {
		t.Fatalf("claims %+v", got)
	}
}

func policyVerifier(t *testing.T, now time.Time, opts ...jwt.VerifierOption) *jwt.Verifier {
	t.Helper()
	base := []jwt.VerifierOption{
		jwt.WithVerifyHS256Keyset(hsKeyset(t)),
		jwt.WithClock(clock.NewMock(now)),
	}
	v, err := jwt.NewVerifier(append(base, opts...)...)
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	return v
}

func signClaims(t *testing.T, c any) string {
	t.Helper()
	s, err := jwt.NewSigner(jwt.WithHS256Keyset(hsKeyset(t)))
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	tok, err := s.Sign(c)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	return tok
}

func TestExpiryPolicy(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_700_000_000, 0)
	exp := jwt.NewNumericDate(now.Add(-time.Minute))
	expired := signClaims(t, jwt.Claims{ExpiresAt: exp})

	t.Run("expired rejected", func(t *testing.T) {
		t.Parallel()
		v := policyVerifier(t, now)
		if _, err := jwt.Verify[jwt.Claims](t.Context(), v, expired); !errors.Is(err, jwt.ErrExpired) {
			t.Fatalf("got %v, want ErrExpired", err)
		}
	})

	t.Run("within default 30s leeway accepted", func(t *testing.T) {
		t.Parallel()
		tok := signClaims(t, jwt.Claims{ExpiresAt: jwt.NewNumericDate(now.Add(-29 * time.Second))})
		v := policyVerifier(t, now)
		if _, err := jwt.Verify[jwt.Claims](t.Context(), v, tok); err != nil {
			t.Fatalf("leeway not applied: %v", err)
		}
	})

	t.Run("exactly exp+leeway rejected", func(t *testing.T) {
		t.Parallel()
		tok := signClaims(t, jwt.Claims{ExpiresAt: jwt.NewNumericDate(now.Add(-30 * time.Second))})
		v := policyVerifier(t, now)
		if _, err := jwt.Verify[jwt.Claims](t.Context(), v, tok); !errors.Is(err, jwt.ErrExpired) {
			t.Fatalf("got %v, want ErrExpired at boundary", err)
		}
	})

	t.Run("custom leeway", func(t *testing.T) {
		t.Parallel()
		v := policyVerifier(t, now, jwt.WithLeeway(2*time.Minute))
		if _, err := jwt.Verify[jwt.Claims](t.Context(), v, expired); err != nil {
			t.Fatalf("2m leeway should accept 1m-expired: %v", err)
		}
	})

	t.Run("missing exp rejected by default", func(t *testing.T) {
		t.Parallel()
		tok := signClaims(t, jwt.Claims{Subject: "user-1"})
		v := policyVerifier(t, now)
		if _, err := jwt.Verify[jwt.Claims](t.Context(), v, tok); !errors.Is(err, jwt.ErrExpired) {
			t.Fatalf("got %v, want ErrExpired for missing exp", err)
		}
	})

	t.Run("missing exp allowed with WithoutExpiry", func(t *testing.T) {
		t.Parallel()
		tok := signClaims(t, jwt.Claims{Subject: "user-1"})
		v := policyVerifier(t, now, jwt.WithoutExpiry())
		if _, err := jwt.Verify[jwt.Claims](t.Context(), v, tok); err != nil {
			t.Fatalf("WithoutExpiry: %v", err)
		}
	})
}

func TestNotBeforePolicy(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_700_000_000, 0)
	future := jwt.NewNumericDate(now.Add(time.Hour))
	exp := jwt.NewNumericDate(now.Add(2 * time.Hour))

	t.Run("future nbf rejected", func(t *testing.T) {
		t.Parallel()
		tok := signClaims(t, jwt.Claims{NotBefore: future, ExpiresAt: exp})
		v := policyVerifier(t, now)
		if _, err := jwt.Verify[jwt.Claims](t.Context(), v, tok); !errors.Is(err, jwt.ErrNotYetValid) {
			t.Fatalf("got %v, want ErrNotYetValid", err)
		}
	})

	t.Run("nbf within leeway accepted", func(t *testing.T) {
		t.Parallel()
		tok := signClaims(t, jwt.Claims{NotBefore: jwt.NewNumericDate(now.Add(29 * time.Second)), ExpiresAt: exp})
		v := policyVerifier(t, now)
		if _, err := jwt.Verify[jwt.Claims](t.Context(), v, tok); err != nil {
			t.Fatalf("nbf leeway: %v", err)
		}
	})
}

func TestIssuerAudiencePolicy(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_700_000_000, 0)
	claims := jwt.Claims{
		Issuer:    "https://api.example.com",
		Audience:  jwt.Audience{"my-app", "other"},
		ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
	}
	tok := signClaims(t, claims)

	cases := map[string]struct {
		opts []jwt.VerifierOption
		want error
	}{
		"iss match":       {[]jwt.VerifierOption{jwt.WithIssuer("https://api.example.com")}, nil},
		"iss mismatch":    {[]jwt.VerifierOption{jwt.WithIssuer("https://evil.example.com")}, jwt.ErrIssuerMismatch},
		"aud contains":    {[]jwt.VerifierOption{jwt.WithAudience("my-app")}, nil},
		"aud missing":     {[]jwt.VerifierOption{jwt.WithAudience("mobile")}, jwt.ErrAudienceMismatch},
		"no policy":       {nil, nil},
		"both mismatched": {[]jwt.VerifierOption{jwt.WithIssuer("x"), jwt.WithAudience("y")}, jwt.ErrIssuerMismatch},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			v := policyVerifier(t, now, tc.opts...)
			_, err := jwt.Verify[jwt.Claims](t.Context(), v, tok)
			if tc.want == nil && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}
