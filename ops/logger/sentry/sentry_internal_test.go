package sentry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/ops/logger"
)

type ctxKey struct{}

func TestNewBuildHandlerErrorReturnsUsableLogger(t *testing.T) {
	var buf bytes.Buffer
	build := func(Config) (slog.Handler, error) { return nil, errors.New("init boom") }

	cfg := DefaultConfig()
	cfg.DSN = "https://publicKey@o0.ingest.sentry.io/0" // non-empty → build is attempted

	log, fl, err := newWith(build, WithConfig(cfg), WithOutput(&buf))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSentryInit)
	require.NotNil(t, log) // graceful: still usable
	require.NotNil(t, fl)

	log.Info("survives")
	assert.Contains(t, buf.String(), "survives")
	require.NoError(t, fl(context.Background())) // no-op flush on the failed path
}

func TestNewWithFanOutAndExtraction(t *testing.T) {
	var primary, fake bytes.Buffer
	// Fake Sentry handler with nil opts → default Info level; the test logs at Info so the
	// record is captured. A debug-level log would be dropped by this handler.
	fakeHandler := slog.NewJSONHandler(&fake, nil)
	build := func(Config) (slog.Handler, error) { return fakeHandler, nil }

	reqID := func(ctx context.Context) (slog.Attr, bool) {
		if v, ok := ctx.Value(ctxKey{}).(string); ok {
			return slog.String("request_id", v), true
		}
		return slog.Attr{}, false
	}

	cfg := DefaultConfig()
	cfg.Config = logger.Config{Level: "debug", Format: "json"} // primary as JSON
	cfg.DSN = "https://publicKey@o0.ingest.sentry.io/0"        // non-empty → triggers build

	log, fl, err := newWith(build,
		WithConfig(cfg),
		WithOutput(&primary),
		WithContextExtractors(reqID),
	)
	require.NoError(t, err)
	require.NotNil(t, fl)

	ctx := context.WithValue(context.Background(), ctxKey{}, "abc-123")
	log.InfoContext(ctx, "hello")

	assert.Contains(t, primary.String(), "hello") // primary destination
	require.NotEmpty(t, fake.Bytes(), "fake Sentry handler received no record")
	var m map[string]any
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(fake.Bytes()), &m))
	assert.Equal(t, "hello", m["msg"])
	assert.Equal(t, "abc-123", m["request_id"]) // Sentry branch got extracted attr
}
