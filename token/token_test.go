package token_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/clock"
	"github.com/dmitrymomot/forge/keyset"
	"github.com/dmitrymomot/forge/secret"
	"github.com/dmitrymomot/forge/token"
)

type Reset struct {
	UserID string `json:"uid"`
}

func TestIssueParse_RoundTrip(t *testing.T) {
	c, err := token.New[Reset]([]byte("0123456789abcdef"), token.WithPurpose("pwreset"))
	require.NoError(t, err)

	tok, err := c.Issue(Reset{UserID: "u_123"})
	require.NoError(t, err)

	got, err := c.Parse(tok)
	require.NoError(t, err)
	assert.Equal(t, "u_123", got.UserID)
}

func TestParse_Expired(t *testing.T) {
	m := clock.NewMock(time.Unix(1_000_000, 0))
	c, err := token.New[Reset]([]byte("0123456789abcdef"),
		token.WithTTL(15*time.Minute), token.WithClock(m))
	require.NoError(t, err)

	tok, err := c.Issue(Reset{UserID: "u_1"})
	require.NoError(t, err)

	m.Advance(16 * time.Minute)
	_, err = c.Parse(tok)
	require.ErrorIs(t, err, token.ErrExpired)
}

func TestParse_NotYetExpired(t *testing.T) {
	m := clock.NewMock(time.Unix(1_000_000, 0))
	c, err := token.New[Reset]([]byte("0123456789abcdef"),
		token.WithTTL(15*time.Minute), token.WithClock(m))
	require.NoError(t, err)
	tok, err := c.Issue(Reset{UserID: "u_1"})
	require.NoError(t, err)

	m.Advance(14 * time.Minute)
	got, err := c.Parse(tok)
	require.NoError(t, err)
	assert.Equal(t, "u_1", got.UserID)
}

func TestParse_Tampered(t *testing.T) {
	c, err := token.New[Reset]([]byte("0123456789abcdef"))
	require.NoError(t, err)
	tok, err := c.Issue(Reset{UserID: "u_1"})
	require.NoError(t, err)

	// Flip the first character of the body segment.
	b := []byte(tok)
	if b[0] == 'A' {
		b[0] = 'B'
	} else {
		b[0] = 'A'
	}
	_, err = c.Parse(string(b))
	require.ErrorIs(t, err, token.ErrBadSignature)
}

func TestParse_WrongPurpose(t *testing.T) {
	key := []byte("0123456789abcdef")
	issuer, err := token.New[Reset](key, token.WithPurpose("pwreset"))
	require.NoError(t, err)
	tok, err := issuer.Issue(Reset{UserID: "u_1"})
	require.NoError(t, err)

	other, err := token.New[Reset](key, token.WithPurpose("magiclink"))
	require.NoError(t, err)
	_, err = other.Parse(tok)
	require.ErrorIs(t, err, token.ErrWrongPurpose)
}

func TestParse_Malformed(t *testing.T) {
	c, err := token.New[Reset]([]byte("0123456789abcdef"))
	require.NoError(t, err)
	_, err = c.Parse("garbage-no-dots")
	require.ErrorIs(t, err, token.ErrMalformed)
}

func TestEncrypted_RoundTrip(t *testing.T) {
	box, err := secret.New([]byte("0123456789abcdef0123456789abcdef")) // 32 bytes
	require.NoError(t, err)
	c, err := token.New[Reset]([]byte("0123456789abcdef"), token.WithEncrypt(box))
	require.NoError(t, err)

	tok, err := c.Issue(Reset{UserID: "secret-uid"})
	require.NoError(t, err)
	assert.NotContains(t, tok, "secret-uid") // payload is encrypted, not just signed

	got, err := c.Parse(tok)
	require.NoError(t, err)
	assert.Equal(t, "secret-uid", got.UserID)
}

func TestUniqueTokensForSamePayload(t *testing.T) {
	c, err := token.New[Reset]([]byte("0123456789abcdef"))
	require.NoError(t, err)
	a, err := c.Issue(Reset{UserID: "u_1"})
	require.NoError(t, err)
	b, err := c.Issue(Reset{UserID: "u_1"})
	require.NoError(t, err)
	assert.NotEqual(t, a, b) // per-token nonce
}

func TestFromKeyset_RoundTrip(t *testing.T) {
	ks, err := keyset.New(keyset.WithPrimary(1, []byte("key-v1-secret-bytes")))
	require.NoError(t, err)
	c, err := token.FromKeyset[Reset](ks, token.WithPurpose("invite"))
	require.NoError(t, err)
	tok, err := c.Issue(Reset{UserID: "u_42"})
	require.NoError(t, err)
	got, err := c.Parse(tok)
	require.NoError(t, err)
	assert.Equal(t, "u_42", got.UserID)
}

func TestEncrypted_AEADFailureMapsToBadSignature(t *testing.T) {
	signKey := []byte("0123456789abcdef")
	boxA, err := secret.New([]byte("0123456789abcdef0123456789abcdef")) // 32 bytes
	require.NoError(t, err)
	issuer, err := token.New[Reset](signKey, token.WithEncrypt(boxA))
	require.NoError(t, err)
	tok, err := issuer.Issue(Reset{UserID: "u_1"})
	require.NoError(t, err)

	// Same signing key → signature verifies; different box key → AEAD decrypt fails.
	boxB, err := secret.New([]byte("ffffffffffffffffffffffffffffffff")) // 32 bytes, different key
	require.NoError(t, err)
	parser, err := token.New[Reset](signKey, token.WithEncrypt(boxB))
	require.NoError(t, err)
	_, err = parser.Parse(tok)
	require.ErrorIs(t, err, token.ErrBadSignature)
}
