package apikey_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/apikey"
)

func TestPlaintext_RedactsThroughFmt(t *testing.T) {
	t.Parallel()
	_, secret, err := apikey.Create(context.Background(), mustConfig(t),
		apikey.CreateParams{Subject: "u1"}, discardKey)
	require.NoError(t, err)

	for verb, want := range map[string]string{"%s": "REDACTED", "%v": "REDACTED", "%#v": "REDACTED"} {
		t.Run(verb, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, want, fmt.Sprintf(verb, secret))
		})
	}
}

func TestPlaintext_RedactsThroughJSON(t *testing.T) {
	t.Parallel()
	_, secret, err := apikey.Create(context.Background(), mustConfig(t),
		apikey.CreateParams{Subject: "u1"}, discardKey)
	require.NoError(t, err)

	encoded, err := json.Marshal(map[string]any{"key": secret})
	require.NoError(t, err)
	assert.JSONEq(t, `{"key":"REDACTED"}`, string(encoded))
}

func TestPlaintext_RedactsThroughSlog(t *testing.T) {
	t.Parallel()
	_, secret, err := apikey.Create(context.Background(), mustConfig(t),
		apikey.CreateParams{Subject: "u1"}, discardKey)
	require.NoError(t, err)

	var logged bytes.Buffer
	slog.New(slog.NewTextHandler(&logged, nil)).Info("issued", "key", secret)

	assert.Contains(t, logged.String(), "REDACTED")
	assert.NotContains(t, logged.String(), secret.Expose())
}

func TestPlaintext_ExposeReturnsTheCredential(t *testing.T) {
	t.Parallel()
	cfg := mustConfig(t, apikey.WithPrefix("sk_live"))
	var stored apikey.Key
	_, secret, err := apikey.Create(context.Background(), cfg,
		apikey.CreateParams{Subject: "u1"}, captureKey(&stored))
	require.NoError(t, err)

	identity, err := apikey.Verify(context.Background(), cfg, secret.Expose(), loadsKeyByHash(stored), nil)
	require.NoError(t, err)
	assert.Equal(t, "u1", identity.Subject)
}
