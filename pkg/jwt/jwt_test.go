package jwt_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/pkg/jwt"
)

// testSigningKey is a 32-byte key satisfying the minimum-key requirement of New.
const testSigningKey = "0123456789abcdef0123456789abcdef"

// TestClaims is a custom claims type that embeds StandardClaims (and therefore
// inherits a promoted Valid() method via embedding).
type TestClaims struct {
	jwt.StandardClaims
	Name  string `json:"name,omitempty"`
	Admin bool   `json:"admin,omitempty"`
}

// NoValidClaims is a custom claims type that does NOT embed StandardClaims and does
// NOT implement Valid(). It still uses the registered exp/nbf JSON field names so the
// always-on temporal validation in Parse must apply to it.
type NoValidClaims struct {
	UserID    string `json:"user_id"`
	ExpiresAt int64  `json:"exp,omitempty"`
	NotBefore int64  `json:"nbf,omitempty"`
}

func newTestService(t *testing.T) *jwt.Service {
	t.Helper()
	service, err := jwt.New(jwt.Config{SigningKey: testSigningKey})
	require.NoError(t, err)
	require.NotNil(t, service)
	return service
}

// decodeSegment base64url-decodes a single JWT segment.
func decodeSegment(t *testing.T, segment string) []byte {
	t.Helper()
	data, err := base64.RawURLEncoding.DecodeString(segment)
	require.NoError(t, err)
	return data
}

// signHS256 recomputes the raw HMAC-SHA256 of payload with key, mirroring the
// signature the package would produce. Used to craft adversarial tokens whose
// signature genuinely covers a forged header.
func signHS256(key, payload []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(payload)
	return h.Sum(nil)
}

func TestNew(t *testing.T) {
	t.Parallel()

	t.Run("with valid signing key", func(t *testing.T) {
		t.Parallel()
		service, err := jwt.New(jwt.Config{SigningKey: testSigningKey})
		require.NoError(t, err)
		require.NotNil(t, service)
	})

	t.Run("with empty signing key", func(t *testing.T) {
		t.Parallel()
		service, err := jwt.New(jwt.Config{})
		require.Error(t, err)
		require.ErrorIs(t, err, jwt.ErrMissingSigningKey)
		require.Nil(t, service)
	})

	t.Run("with key shorter than 32 bytes", func(t *testing.T) {
		t.Parallel()
		service, err := jwt.New(jwt.Config{SigningKey: "short-secret"})
		require.Error(t, err)
		require.ErrorIs(t, err, jwt.ErrInvalidSigningKey)
		require.Nil(t, service)
	})

	t.Run("with exactly 32-byte key", func(t *testing.T) {
		t.Parallel()
		key := strings.Repeat("k", 32)
		service, err := jwt.New(jwt.Config{SigningKey: key})
		require.NoError(t, err)
		require.NotNil(t, service)
	})
}

func TestGenerate(t *testing.T) {
	t.Parallel()
	service := newTestService(t)

	t.Run("with standard claims produces a 3-part token with valid header", func(t *testing.T) {
		t.Parallel()
		claims := jwt.StandardClaims{
			Subject:   "user123",
			Issuer:    "saaskit",
			ExpiresAt: time.Now().Add(time.Hour).Unix(),
		}

		token, err := service.Generate(claims)
		require.NoError(t, err)
		require.NotEmpty(t, token)

		// Token must have exactly 3 dot-separated parts (header.claims.signature).
		parts := strings.Split(token, ".")
		require.Len(t, parts, 3)
		for _, p := range parts {
			require.NotEmpty(t, p)
		}

		// The header segment must decode to JSON with typ=JWT and alg=HS256.
		var header jwt.Header
		require.NoError(t, json.Unmarshal(decodeSegment(t, parts[0]), &header))
		require.Equal(t, jwt.HeaderType, header.Type)
		require.Equal(t, jwt.HeaderAlgorithm, header.Algorithm)

		// The claims segment must decode back to the original subject/issuer.
		var decoded jwt.StandardClaims
		require.NoError(t, json.Unmarshal(decodeSegment(t, parts[1]), &decoded))
		require.Equal(t, claims.Subject, decoded.Subject)
		require.Equal(t, claims.Issuer, decoded.Issuer)
	})

	t.Run("with custom claims", func(t *testing.T) {
		t.Parallel()
		claims := TestClaims{
			StandardClaims: jwt.StandardClaims{
				Subject:   "user123",
				Issuer:    "saaskit",
				ExpiresAt: time.Now().Add(time.Hour).Unix(),
			},
			Name:  "John Doe",
			Admin: true,
		}

		token, err := service.Generate(claims)
		require.NoError(t, err)
		require.Len(t, strings.Split(token, "."), 3)
	})

	t.Run("with nil claims", func(t *testing.T) {
		t.Parallel()
		token, err := service.Generate(nil)
		require.Error(t, err)
		require.ErrorIs(t, err, jwt.ErrMissingClaims)
		require.Empty(t, token)
	})
}

func TestParse(t *testing.T) {
	t.Parallel()
	service := newTestService(t)

	t.Run("with standard claims", func(t *testing.T) {
		t.Parallel()
		originalClaims := jwt.StandardClaims{
			Subject:   "user123",
			Issuer:    "saaskit",
			ExpiresAt: time.Now().Add(time.Hour).Unix(),
		}

		token, err := service.Generate(originalClaims)
		require.NoError(t, err)
		require.NotEmpty(t, token)

		var parsedClaims jwt.StandardClaims
		err = service.Parse(token, &parsedClaims)
		require.NoError(t, err)

		require.Equal(t, originalClaims.Subject, parsedClaims.Subject)
		require.Equal(t, originalClaims.Issuer, parsedClaims.Issuer)
		require.Equal(t, originalClaims.ExpiresAt, parsedClaims.ExpiresAt)
	})

	t.Run("with custom claims", func(t *testing.T) {
		t.Parallel()
		originalClaims := TestClaims{
			StandardClaims: jwt.StandardClaims{
				Subject:   "user123",
				Issuer:    "saaskit",
				ExpiresAt: time.Now().Add(time.Hour).Unix(),
			},
			Name:  "John Doe",
			Admin: true,
		}

		token, err := service.Generate(originalClaims)
		require.NoError(t, err)
		require.NotEmpty(t, token)

		var parsedClaims TestClaims
		err = service.Parse(token, &parsedClaims)
		require.NoError(t, err)

		require.Equal(t, originalClaims.Subject, parsedClaims.Subject)
		require.Equal(t, originalClaims.Issuer, parsedClaims.Issuer)
		require.Equal(t, originalClaims.ExpiresAt, parsedClaims.ExpiresAt)
		require.Equal(t, originalClaims.Name, parsedClaims.Name)
		require.Equal(t, originalClaims.Admin, parsedClaims.Admin)
	})

	t.Run("with invalid token format", func(t *testing.T) {
		t.Parallel()
		var claims jwt.StandardClaims
		err := service.Parse("invalid-token", &claims)
		require.Error(t, err)
		require.ErrorIs(t, err, jwt.ErrInvalidToken)
	})

	t.Run("with tampered signature segment", func(t *testing.T) {
		t.Parallel()
		originalClaims := jwt.StandardClaims{
			Subject:   "user123",
			ExpiresAt: time.Now().Add(time.Hour).Unix(),
		}

		token, err := service.Generate(originalClaims)
		require.NoError(t, err)
		require.NotEmpty(t, token)

		// Tamper with the signature by changing the last character.
		tampered := token[:len(token)-1] + "X"
		require.NotEqual(t, token, tampered)

		var parsedClaims jwt.StandardClaims
		err = service.Parse(tampered, &parsedClaims)
		require.Error(t, err)
		require.ErrorIs(t, err, jwt.ErrInvalidSignature)
	})

	t.Run("with tampered claims segment is rejected by signature check", func(t *testing.T) {
		t.Parallel()
		// Forge a token whose claims segment grants admin without re-signing.
		legit := TestClaims{
			StandardClaims: jwt.StandardClaims{
				Subject:   "user123",
				ExpiresAt: time.Now().Add(time.Hour).Unix(),
			},
			Admin: false,
		}
		token, err := service.Generate(legit)
		require.NoError(t, err)

		parts := strings.Split(token, ".")
		require.Len(t, parts, 3)

		forgedClaims := TestClaims{
			StandardClaims: legit.StandardClaims,
			Admin:          true,
		}
		forgedJSON, err := json.Marshal(forgedClaims)
		require.NoError(t, err)
		forgedSegment := base64.RawURLEncoding.EncodeToString(forgedJSON)
		require.NotEqual(t, parts[1], forgedSegment)

		// Keep the original (now-mismatched) signature; this must be rejected.
		forgedToken := parts[0] + "." + forgedSegment + "." + parts[2]

		var parsed TestClaims
		err = service.Parse(forgedToken, &parsed)
		require.Error(t, err)
		require.ErrorIs(t, err, jwt.ErrInvalidSignature)
		require.False(t, parsed.Admin, "forged claims must not be applied")
	})

	t.Run("with forged alg header and matching signature is rejected by alg check", func(t *testing.T) {
		t.Parallel()
		// Construct a token whose signature genuinely covers a forged "none" header
		// (e.g. an attacker who holds the symmetric key, or a key-confusion scenario).
		// The algorithm check must reject it even though the signature is valid for
		// these exact bytes, so the rejection is not merely a signature mismatch.
		forgedHeader := jwt.Header{Type: jwt.HeaderType, Algorithm: "none"}
		headerJSON, err := json.Marshal(forgedHeader)
		require.NoError(t, err)
		headerSegment := base64.RawURLEncoding.EncodeToString(headerJSON)

		claims := jwt.StandardClaims{Subject: "user123", ExpiresAt: time.Now().Add(time.Hour).Unix()}
		claimsJSON, err := json.Marshal(claims)
		require.NoError(t, err)
		claimsSegment := base64.RawURLEncoding.EncodeToString(claimsJSON)

		payload := headerSegment + "." + claimsSegment
		mac := signHS256([]byte(testSigningKey), []byte(payload))
		sigSegment := base64.RawURLEncoding.EncodeToString(mac)

		forgedToken := payload + "." + sigSegment

		var parsed jwt.StandardClaims
		err = service.Parse(forgedToken, &parsed)
		require.Error(t, err)
		require.ErrorIs(t, err, jwt.ErrUnexpectedSigningMethod)
	})

	t.Run("with expired token", func(t *testing.T) {
		t.Parallel()
		expiredClaims := jwt.StandardClaims{
			Subject:   "user123",
			ExpiresAt: time.Now().Add(-time.Hour).Unix(),
		}

		token, err := service.Generate(expiredClaims)
		require.NoError(t, err)

		var parsedClaims jwt.StandardClaims
		err = service.Parse(token, &parsedClaims)
		require.Error(t, err)
		require.ErrorIs(t, err, jwt.ErrExpiredToken)
	})

	t.Run("with not-yet-valid token returns ErrTokenNotYetValid", func(t *testing.T) {
		t.Parallel()
		futureClaims := jwt.StandardClaims{
			Subject:   "user123",
			ExpiresAt: time.Now().Add(time.Hour).Unix(),
			NotBefore: time.Now().Add(time.Hour).Unix(),
		}

		token, err := service.Generate(futureClaims)
		require.NoError(t, err)

		var parsedClaims jwt.StandardClaims
		err = service.Parse(token, &parsedClaims)
		require.Error(t, err)
		require.ErrorIs(t, err, jwt.ErrTokenNotYetValid)
	})
}

// TestParseTemporalAlwaysEnforced proves the security fix: temporal (exp/nbf)
// validation runs even when the claims type does NOT implement Valid().
func TestParseTemporalAlwaysEnforced(t *testing.T) {
	t.Parallel()
	service := newTestService(t)

	t.Run("expired token rejected for claims type without Valid()", func(t *testing.T) {
		t.Parallel()
		expired := NoValidClaims{
			UserID:    "user123",
			ExpiresAt: time.Now().Add(-time.Hour).Unix(),
		}

		token, err := service.Generate(expired)
		require.NoError(t, err)

		var parsed NoValidClaims
		err = service.Parse(token, &parsed)
		require.Error(t, err)
		require.ErrorIs(t, err, jwt.ErrExpiredToken)
	})

	t.Run("not-yet-valid token rejected for claims type without Valid()", func(t *testing.T) {
		t.Parallel()
		future := NoValidClaims{
			UserID:    "user123",
			ExpiresAt: time.Now().Add(time.Hour).Unix(),
			NotBefore: time.Now().Add(time.Hour).Unix(),
		}

		token, err := service.Generate(future)
		require.NoError(t, err)

		var parsed NoValidClaims
		err = service.Parse(token, &parsed)
		require.Error(t, err)
		require.ErrorIs(t, err, jwt.ErrTokenNotYetValid)
	})

	t.Run("valid token accepted for claims type without Valid()", func(t *testing.T) {
		t.Parallel()
		valid := NoValidClaims{
			UserID:    "user123",
			ExpiresAt: time.Now().Add(time.Hour).Unix(),
		}

		token, err := service.Generate(valid)
		require.NoError(t, err)

		var parsed NoValidClaims
		err = service.Parse(token, &parsed)
		require.NoError(t, err)
		require.Equal(t, "user123", parsed.UserID)
	})
}

// TestParseLeeway verifies Config.Leeway tolerates small clock skew on exp/nbf.
func TestParseLeeway(t *testing.T) {
	t.Parallel()

	t.Run("token just past exp is accepted within leeway", func(t *testing.T) {
		t.Parallel()
		service, err := jwt.New(jwt.Config{SigningKey: testSigningKey, Leeway: time.Minute})
		require.NoError(t, err)

		// Expired 30s ago; within the 1-minute leeway window.
		claims := jwt.StandardClaims{
			Subject:   "user123",
			ExpiresAt: time.Now().Add(-30 * time.Second).Unix(),
		}
		token, err := service.Generate(claims)
		require.NoError(t, err)

		var parsed jwt.StandardClaims
		err = service.Parse(token, &parsed)
		require.NoError(t, err)
	})

	t.Run("token past exp beyond leeway is rejected", func(t *testing.T) {
		t.Parallel()
		service, err := jwt.New(jwt.Config{SigningKey: testSigningKey, Leeway: time.Minute})
		require.NoError(t, err)

		claims := jwt.StandardClaims{
			Subject:   "user123",
			ExpiresAt: time.Now().Add(-2 * time.Minute).Unix(),
		}
		token, err := service.Generate(claims)
		require.NoError(t, err)

		var parsed jwt.StandardClaims
		err = service.Parse(token, &parsed)
		require.Error(t, err)
		require.ErrorIs(t, err, jwt.ErrExpiredToken)
	})

	t.Run("nbf within leeway window is accepted", func(t *testing.T) {
		t.Parallel()
		service, err := jwt.New(jwt.Config{SigningKey: testSigningKey, Leeway: time.Minute})
		require.NoError(t, err)

		claims := jwt.StandardClaims{
			Subject:   "user123",
			ExpiresAt: time.Now().Add(time.Hour).Unix(),
			NotBefore: time.Now().Add(30 * time.Second).Unix(),
		}
		token, err := service.Generate(claims)
		require.NoError(t, err)

		var parsed jwt.StandardClaims
		err = service.Parse(token, &parsed)
		require.NoError(t, err)
	})

	t.Run("embedded StandardClaims within leeway is accepted (promoted Valid not stricter)", func(t *testing.T) {
		t.Parallel()
		service, err := jwt.New(jwt.Config{SigningKey: testSigningKey, Leeway: time.Minute})
		require.NoError(t, err)

		// TestClaims embeds StandardClaims, so it has a promoted zero-leeway Valid().
		// The leeway-aware Parse check must still accept a token within the window.
		claims := TestClaims{
			StandardClaims: jwt.StandardClaims{
				Subject:   "user123",
				ExpiresAt: time.Now().Add(-30 * time.Second).Unix(),
			},
			Name: "John Doe",
		}
		token, err := service.Generate(claims)
		require.NoError(t, err)

		var parsed TestClaims
		err = service.Parse(token, &parsed)
		require.NoError(t, err)
		require.Equal(t, "John Doe", parsed.Name)
	})
}

func TestSigningKeyDifference(t *testing.T) {
	t.Parallel()

	service1, err := jwt.New(jwt.Config{SigningKey: strings.Repeat("a", 32)})
	require.NoError(t, err)

	service2, err := jwt.New(jwt.Config{SigningKey: strings.Repeat("b", 32)})
	require.NoError(t, err)

	claims := jwt.StandardClaims{
		Subject:   "user123",
		Issuer:    "saaskit",
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	}

	token, err := service1.Generate(claims)
	require.NoError(t, err)
	require.NotEmpty(t, token)

	var parsedClaims jwt.StandardClaims
	err = service2.Parse(token, &parsedClaims)
	require.Error(t, err)
	require.ErrorIs(t, err, jwt.ErrInvalidSignature)
}
