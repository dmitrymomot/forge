package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseMinLevel(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want slog.Level
	}{
		{"debug", "debug", slog.LevelDebug},
		{"info", "info", slog.LevelInfo},
		{"warn", "warn", slog.LevelWarn},
		{"warning alias", "warning", slog.LevelWarn},
		{"error", "error", slog.LevelError},
		{"uppercase", "ERROR", slog.LevelError},
		{"trimmed", "  info  ", slog.LevelInfo},
		{"unknown defaults to warn", "verbose", slog.LevelWarn},
		{"empty defaults to warn", "", slog.LevelWarn},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, parseMinLevel(tc.in))
		})
	}
}

func TestLevelsFrom(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		min  slog.Level
		want []slog.Level
	}{
		{
			"from debug includes all",
			slog.LevelDebug,
			[]slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError},
		},
		{
			"from warn drops debug and info",
			slog.LevelWarn,
			[]slog.Level{slog.LevelWarn, slog.LevelError},
		},
		{
			"from error only error",
			slog.LevelError,
			[]slog.Level{slog.LevelError},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, levelsFrom(tc.min))
		})
	}
}

// captureStdout runs fn with os.Stdout redirected to a pipe and returns everything written.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = orig })

	done := make(chan []byte, 1)
	go func() {
		var b bytes.Buffer
		_, _ = io.Copy(&b, r)
		done <- b.Bytes()
	}()

	fn()
	require.NoError(t, w.Close())
	out := <-done
	os.Stdout = orig
	return string(out)
}

func TestNewWithSentry_EmptyDSNFallback(t *testing.T) {
	// Not parallel: this test redirects the process-global os.Stdout.

	reqIDExtractor := func(ctx context.Context) (slog.Attr, bool) {
		if v, ok := ctx.Value(testKey("rid")).(string); ok && v != "" {
			return slog.String("request_id", v), true
		}
		return slog.Attr{}, false
	}

	var closer SentryCloser
	out := captureStdout(t, func() {
		cfg := SentryConfig{DSN: "", MinLevel: "info"}
		var log *slog.Logger
		log, closer = NewWithSentry(cfg, reqIDExtractor)

		ctx := context.WithValue(context.Background(), testKey("rid"), "fallback-1")
		log.InfoContext(ctx, "no sentry", slog.Int("n", 7))
	})

	require.NotEmpty(t, out, "fallback must still write to stdout")

	var m map[string]any
	require.NoError(t, json.Unmarshal([]byte(lastLine(out)), &m), "stdout output must be JSON: %s", out)
	require.Equal(t, "no sentry", m["msg"])
	require.Equal(t, "fallback-1", m["request_id"], "extractors must still apply on the empty-DSN fallback path")
	require.EqualValues(t, 7, m["n"])

	// The closer on the fallback path must be a no-op that returns nil.
	require.NotNil(t, closer)
	require.NoError(t, closer(time.Second))
}

func TestNewWithSentry_EmptyDSNRespectsMinLevel(t *testing.T) {
	// Not parallel: redirects os.Stdout.

	out := captureStdout(t, func() {
		cfg := SentryConfig{DSN: "", MinLevel: "error"}
		log, _ := NewWithSentry(cfg)
		// Below MinLevel: must be dropped from stdout.
		log.InfoContext(context.Background(), "dropped info")
		log.WarnContext(context.Background(), "dropped warn")
		// At MinLevel: must be emitted.
		log.ErrorContext(context.Background(), "kept error")
	})

	require.NotContains(t, out, "dropped info", "sub-MinLevel info must be dropped from stdout")
	require.NotContains(t, out, "dropped warn", "sub-MinLevel warn must be dropped from stdout")
	require.Contains(t, out, "kept error", "error at MinLevel must be emitted")

	var m map[string]any
	require.NoError(t, json.Unmarshal([]byte(lastLine(out)), &m))
	require.Equal(t, "ERROR", m["level"])
	require.Equal(t, "kept error", m["msg"])
}

type testKey string

// lastLine returns the last non-empty line of s.
func lastLine(s string) string {
	lines := bytes.Split(bytes.TrimSpace([]byte(s)), []byte("\n"))
	if len(lines) == 0 {
		return ""
	}
	return string(lines[len(lines)-1])
}
