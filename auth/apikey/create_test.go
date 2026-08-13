package apikey_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/apikey"
)

const base62Alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

func TestCreate_PlaintextLength(t *testing.T) {
	t.Parallel()
	_, plaintext := issueKey(t, mustConfig(t, apikey.WithPrefix("sk_live")), apikey.CreateParams{Subject: "u1"})
	assert.Len(t, plaintext, len("sk_live")+1+43+6)
}

func TestCreate_PlaintextCarriesConfiguredPrefix(t *testing.T) {
	t.Parallel()
	_, plaintext := issueKey(t, mustConfig(t, apikey.WithPrefix("sk_live")), apikey.CreateParams{Subject: "u1"})
	assert.True(t, strings.HasPrefix(plaintext, "sk_live_"))
}

func TestCreate_PlaintextBodyIsBase62(t *testing.T) {
	t.Parallel()
	_, plaintext := issueKey(t, mustConfig(t, apikey.WithPrefix("sk_live")), apikey.CreateParams{Subject: "u1"})
	for _, c := range plaintext[len("sk_live_"):] {
		assert.Contains(t, base62Alphabet, string(c))
	}
}

func TestCreate_PlaintextIsUniquePerCall(t *testing.T) {
	t.Parallel()
	cfg := mustConfig(t)
	_, first := issueKey(t, cfg, apikey.CreateParams{Subject: "u1"})
	_, second := issueKey(t, cfg, apikey.CreateParams{Subject: "u1"})
	assert.NotEqual(t, first, second)
}

func TestCreate_PreviewIsPlaintextPrefix(t *testing.T) {
	t.Parallel()
	k, plaintext := issueKey(t, mustConfig(t), apikey.CreateParams{Subject: "u1"})
	assert.Equal(t, plaintext[:12], k.Preview)
}

// TestCreate_RecordCarriesHashNotPlaintext pins the at-rest rule: the
// record handed to the save effect never contains the secret.
func TestCreate_RecordCarriesHashNotPlaintext(t *testing.T) {
	t.Parallel()
	var stored apikey.Key
	_, plaintext, err := expose(apikey.Create(context.Background(), mustConfig(t),
		apikey.CreateParams{Subject: "u1"}, captureKey(&stored)))
	require.NoError(t, err)

	assert.NotContains(t, stored.Hash, plaintext[8:20])
	assert.Len(t, stored.Hash, 64)
}

func TestCreate_StampsIDAndCreatedAt(t *testing.T) {
	t.Parallel()
	k, _ := issueKey(t, mustConfig(t), apikey.CreateParams{Subject: "u1"})
	assert.False(t, k.ID.IsZero())
	assert.False(t, k.CreatedAt.IsZero())
}

func TestCreate_CopiesParamsIntoRecord(t *testing.T) {
	t.Parallel()
	var stored apikey.Key
	expiry := time.Now().UTC().Add(time.Hour)
	_, _, err := expose(apikey.Create(context.Background(), mustConfig(t), apikey.CreateParams{
		Subject: "user_42", Tenant: "org_7", Name: "CI deploy",
		Scopes: []string{"deploy:write"}, Meta: map[string]string{"env": "prod"},
		ExpiresAt: expiry,
	}, captureKey(&stored)))
	require.NoError(t, err)

	assert.Equal(t, "user_42", stored.Subject)
	assert.Equal(t, "org_7", stored.Tenant)
	assert.Equal(t, "CI deploy", stored.Name)
	assert.Equal(t, []string{"deploy:write"}, stored.Scopes)
	assert.Equal(t, map[string]string{"env": "prod"}, stored.Meta)
	assert.True(t, stored.ExpiresAt.Equal(expiry))
}

// TestCreate_RecordDoesNotAliasParams pins that the record the save effect
// persists cannot be mutated afterwards through the caller's own slices.
func TestCreate_RecordDoesNotAliasParams(t *testing.T) {
	t.Parallel()
	scopes := []string{"read"}
	meta := map[string]string{"env": "prod"}
	var stored apikey.Key
	_, _, err := expose(apikey.Create(context.Background(), mustConfig(t),
		apikey.CreateParams{Subject: "u1", Scopes: scopes, Meta: meta}, captureKey(&stored)))
	require.NoError(t, err)

	scopes[0] = "mutated"
	meta["env"] = "mutated"

	assert.Equal(t, "read", stored.Scopes[0])
	assert.Equal(t, "prod", stored.Meta["env"])
}

func TestCreate_RejectsEmptySubject(t *testing.T) {
	t.Parallel()
	_, _, err := expose(apikey.Create(context.Background(), mustConfig(t),
		apikey.CreateParams{}, discardKey))
	assert.ErrorIs(t, err, apikey.ErrSubjectRequired)
}

// TestCreate_SaveClosureOwnsTheWrite pins the point of the design: the
// call site's own closure performs the write and may carry request-scoped
// data the package never models.
func TestCreate_SaveClosureOwnsTheWrite(t *testing.T) {
	t.Parallel()
	tenantFromRequest := "org_from_request"
	var wroteWith string

	_, _, err := expose(apikey.Create(context.Background(), mustConfig(t),
		apikey.CreateParams{Subject: "u1"},
		func(context.Context, apikey.Key) error {
			wroteWith = tenantFromRequest
			return nil
		}))
	require.NoError(t, err)
	assert.Equal(t, tenantFromRequest, wroteWith)
}

func TestCreate_WrapsSaveError(t *testing.T) {
	t.Parallel()
	errBackend := errors.New("backend down")

	_, _, err := expose(apikey.Create(context.Background(), mustConfig(t),
		apikey.CreateParams{Subject: "u1"},
		func(context.Context, apikey.Key) error { return errBackend }))
	require.Error(t, err)
	assert.ErrorIs(t, err, errBackend)
}

func TestCreate_ReturnsNoPlaintextWhenSaveFails(t *testing.T) {
	t.Parallel()
	_, plaintext, err := expose(apikey.Create(context.Background(), mustConfig(t),
		apikey.CreateParams{Subject: "u1"},
		func(context.Context, apikey.Key) error { return apikey.ErrDuplicate }))
	require.Error(t, err)
	assert.Empty(t, plaintext)
}
