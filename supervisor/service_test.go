package supervisor_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/supervisor"
)

func TestWithServiceFunc_DelegatesNameAndRun(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	type ctxKey struct{}
	var gotVal any
	fn := func(ctx context.Context) error {
		gotVal = ctx.Value(ctxKey{})
		return nil
	}

	ctx := context.WithValue(context.Background(), ctxKey{}, "v")
	err := supervisor.Run(ctx, supervisor.WithServiceFunc("worker", fn), supervisor.WithLogger(logger))
	require.NoError(t, err)

	// gotVal == "v" proves the func ran AND ctx passed straight through.
	assert.Equal(t, "v", gotVal, "fn must be invoked with ctx passed straight through")

	// The name is observable via the structured "service started" log record.
	var named bool
	for line := range bytes.SplitSeq(bytes.TrimSpace(buf.Bytes()), []byte("\n")) {
		var rec map[string]any
		require.NoError(t, json.Unmarshal(line, &rec))
		if rec["msg"] == "service started" && rec["service"] == "worker" {
			named = true
			break
		}
	}
	assert.True(t, named, `expected a "service started" record with service="worker"`)
}

func TestWithServiceFunc_PropagatesError(t *testing.T) {
	sentinel := errors.New("boom")
	fn := func(ctx context.Context) error { return sentinel }
	err := supervisor.Run(context.Background(), supervisor.WithServiceFunc("x", fn), supervisor.WithLogger(discardLogger()))
	require.ErrorIs(t, err, sentinel)
}
