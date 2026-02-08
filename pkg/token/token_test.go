package token_test

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/pkg/token"
)

type EmailVerification struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
	Action string `json:"action"`
}

type InvitePayload struct {
	WorkspaceID string `json:"workspace_id"`
	InviterID   string `json:"inviter_id"`
	Role        string `json:"role"`
}

func TestGenerateToken(t *testing.T) {
	t.Parallel()
	secret := []byte("test-secret-key-32-bytes-long!!!")

	t.Run("with struct payload", func(t *testing.T) {
		t.Parallel()
		payload := EmailVerification{
			UserID: "usr_abc123",
			Email:  "user@example.com",
			Action: "verify_email",
		}

		tok, err := token.GenerateToken(payload, secret)
		require.NoError(t, err)
		require.NotEmpty(t, tok)

		parts := strings.Split(tok, ".")
		assert.Len(t, parts, 2)
	})

	t.Run("with map payload", func(t *testing.T) {
		t.Parallel()
		payload := map[string]any{
			"user_id": "usr_abc123",
			"action":  "reset_password",
		}

		tok, err := token.GenerateToken(payload, secret)
		require.NoError(t, err)
		require.NotEmpty(t, tok)
	})

	t.Run("with string payload", func(t *testing.T) {
		t.Parallel()
		tok, err := token.GenerateToken("hello", secret)
		require.NoError(t, err)
		require.NotEmpty(t, tok)
	})

	t.Run("with nil secret", func(t *testing.T) {
		t.Parallel()
		tok, err := token.GenerateToken("payload", nil)
		require.ErrorIs(t, err, token.ErrEmptySecret)
		require.Empty(t, tok)
	})

	t.Run("with empty secret", func(t *testing.T) {
		t.Parallel()
		tok, err := token.GenerateToken("payload", []byte{})
		require.ErrorIs(t, err, token.ErrEmptySecret)
		require.Empty(t, tok)
	})
}

func TestParseToken(t *testing.T) {
	t.Parallel()
	secret := []byte("test-secret-key-32-bytes-long!!!")

	t.Run("round trip with struct", func(t *testing.T) {
		t.Parallel()
		original := EmailVerification{
			UserID: "usr_abc123",
			Email:  "user@example.com",
			Action: "verify_email",
		}

		tok, err := token.GenerateToken(original, secret)
		require.NoError(t, err)

		result, err := token.ParseToken[EmailVerification](tok, secret)
		require.NoError(t, err)
		require.NotNil(t, result)

		assert.Equal(t, original.UserID, result.UserID)
		assert.Equal(t, original.Email, result.Email)
		assert.Equal(t, original.Action, result.Action)
	})

	t.Run("round trip with nested struct", func(t *testing.T) {
		t.Parallel()
		type Nested struct {
			ID   string            `json:"id"`
			Tags []string          `json:"tags"`
			Meta map[string]string `json:"meta"`
		}

		original := Nested{
			ID:   "item_123",
			Tags: []string{"a", "b", "c"},
			Meta: map[string]string{"key": "value"},
		}

		tok, err := token.GenerateToken(original, secret)
		require.NoError(t, err)

		result, err := token.ParseToken[Nested](tok, secret)
		require.NoError(t, err)
		require.NotNil(t, result)

		assert.Equal(t, original.ID, result.ID)
		assert.Equal(t, original.Tags, result.Tags)
		assert.Equal(t, original.Meta, result.Meta)
	})

	t.Run("with invalid token format", func(t *testing.T) {
		t.Parallel()
		result, err := token.ParseToken[EmailVerification]("invalidtoken", secret)
		require.ErrorIs(t, err, token.ErrInvalidToken)
		require.Nil(t, result)
	})

	t.Run("with empty payload segment", func(t *testing.T) {
		t.Parallel()
		result, err := token.ParseToken[EmailVerification](".signature", secret)
		require.ErrorIs(t, err, token.ErrInvalidToken)
		require.Nil(t, result)
	})

	t.Run("with empty signature segment", func(t *testing.T) {
		t.Parallel()
		result, err := token.ParseToken[EmailVerification]("payload.", secret)
		require.ErrorIs(t, err, token.ErrInvalidToken)
		require.Nil(t, result)
	})

	t.Run("with tampered payload", func(t *testing.T) {
		t.Parallel()
		tok, err := token.GenerateToken(EmailVerification{
			UserID: "usr_abc123",
			Email:  "user@example.com",
			Action: "verify_email",
		}, secret)
		require.NoError(t, err)

		parts := strings.SplitN(tok, ".", 2)
		require.Len(t, parts, 2)

		// Tamper with the payload
		tampered := parts[0] + "X." + parts[1]

		result, err := token.ParseToken[EmailVerification](tampered, secret)
		require.ErrorIs(t, err, token.ErrSignatureInvalid)
		require.Nil(t, result)
	})

	t.Run("with tampered signature", func(t *testing.T) {
		t.Parallel()
		tok, err := token.GenerateToken(EmailVerification{
			UserID: "usr_abc123",
			Email:  "user@example.com",
			Action: "verify_email",
		}, secret)
		require.NoError(t, err)

		// Change last character of the token (signature part)
		tampered := tok[:len(tok)-1] + "X"

		result, err := token.ParseToken[EmailVerification](tampered, secret)
		require.ErrorIs(t, err, token.ErrSignatureInvalid)
		require.Nil(t, result)
	})

	t.Run("with wrong secret", func(t *testing.T) {
		t.Parallel()
		tok, err := token.GenerateToken(EmailVerification{
			UserID: "usr_abc123",
			Email:  "user@example.com",
			Action: "verify_email",
		}, secret)
		require.NoError(t, err)

		result, err := token.ParseToken[EmailVerification](tok, []byte("wrong-secret"))
		require.ErrorIs(t, err, token.ErrSignatureInvalid)
		require.Nil(t, result)
	})

	t.Run("with nil secret", func(t *testing.T) {
		t.Parallel()
		result, err := token.ParseToken[EmailVerification]("any.token", nil)
		require.ErrorIs(t, err, token.ErrEmptySecret)
		require.Nil(t, result)
	})

	t.Run("with empty secret", func(t *testing.T) {
		t.Parallel()
		result, err := token.ParseToken[EmailVerification]("any.token", []byte{})
		require.ErrorIs(t, err, token.ErrEmptySecret)
		require.Nil(t, result)
	})

	t.Run("with empty token", func(t *testing.T) {
		t.Parallel()
		result, err := token.ParseToken[EmailVerification]("", secret)
		require.ErrorIs(t, err, token.ErrInvalidToken)
		require.Nil(t, result)
	})
}

func TestDifferentSecrets(t *testing.T) {
	t.Parallel()

	payload := EmailVerification{
		UserID: "usr_abc123",
		Email:  "user@example.com",
		Action: "verify_email",
	}

	tok, err := token.GenerateToken(payload, []byte("secret-one"))
	require.NoError(t, err)

	result, err := token.ParseToken[EmailVerification](tok, []byte("secret-two"))
	require.ErrorIs(t, err, token.ErrSignatureInvalid)
	require.Nil(t, result)
}

func TestTokenFormat(t *testing.T) {
	t.Parallel()
	secret := []byte("test-secret-key-32-bytes-long!!!")

	payload := EmailVerification{
		UserID: "usr_abc123",
		Email:  "user@example.com",
		Action: "verify_email",
	}

	tok, err := token.GenerateToken(payload, secret)
	require.NoError(t, err)

	parts := strings.Split(tok, ".")
	require.Len(t, parts, 2)

	// Payload is valid base64url
	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	require.NoError(t, err)

	// Decoded payload is valid JSON matching the input
	var decoded EmailVerification
	err = json.Unmarshal(payloadJSON, &decoded)
	require.NoError(t, err)
	assert.Equal(t, payload.UserID, decoded.UserID)
	assert.Equal(t, payload.Email, decoded.Email)
	assert.Equal(t, payload.Action, decoded.Action)

	// Signature is valid base64url
	sigBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	require.NoError(t, err)
	assert.Len(t, sigBytes, 8)
}

func TestTokenURLSafety(t *testing.T) {
	t.Parallel()
	secret := []byte("test-secret-key-32-bytes-long!!!")

	// Use a payload with characters that would produce +/= in standard base64
	payload := map[string]string{
		"data": "special chars: <>?&=+/ and more",
		"url":  "https://example.com/path?key=value&other=123",
	}

	tok, err := token.GenerateToken(payload, secret)
	require.NoError(t, err)

	assert.NotContains(t, tok, "+")
	assert.NotContains(t, tok, "/")
	assert.NotContains(t, tok, "=")
}

func TestGenerateTokenDeterministic(t *testing.T) {
	t.Parallel()
	secret := []byte("test-secret-key-32-bytes-long!!!")

	payload := EmailVerification{
		UserID: "usr_abc123",
		Email:  "user@example.com",
		Action: "verify_email",
	}

	tok1, err := token.GenerateToken(payload, secret)
	require.NoError(t, err)

	tok2, err := token.GenerateToken(payload, secret)
	require.NoError(t, err)

	assert.Equal(t, tok1, tok2)
}

func TestParseTokenTypeVariety(t *testing.T) {
	t.Parallel()
	secret := []byte("test-secret-key-32-bytes-long!!!")

	t.Run("int payload", func(t *testing.T) {
		t.Parallel()
		tok, err := token.GenerateToken(42, secret)
		require.NoError(t, err)

		result, err := token.ParseToken[int](tok, secret)
		require.NoError(t, err)
		assert.Equal(t, 42, *result)
	})

	t.Run("string slice payload", func(t *testing.T) {
		t.Parallel()
		original := []string{"a", "b", "c"}

		tok, err := token.GenerateToken(original, secret)
		require.NoError(t, err)

		result, err := token.ParseToken[[]string](tok, secret)
		require.NoError(t, err)
		assert.Equal(t, original, *result)
	})

	t.Run("map payload", func(t *testing.T) {
		t.Parallel()
		original := map[string]string{"key": "value", "foo": "bar"}

		tok, err := token.GenerateToken(original, secret)
		require.NoError(t, err)

		result, err := token.ParseToken[map[string]string](tok, secret)
		require.NoError(t, err)
		assert.Equal(t, original, *result)
	})

	t.Run("bool payload", func(t *testing.T) {
		t.Parallel()
		tok, err := token.GenerateToken(true, secret)
		require.NoError(t, err)

		result, err := token.ParseToken[bool](tok, secret)
		require.NoError(t, err)
		assert.Equal(t, true, *result)
	})
}
