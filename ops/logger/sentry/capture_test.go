package sentry

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	sentry "github.com/getsentry/sentry-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingHandler is a fake "next" handler that records every delegated call.
type recordingHandler struct{ records []slog.Record }

func (h *recordingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.records = append(h.records, r)
	return nil
}
func (h *recordingHandler) WithAttrs(attrs []slog.Attr) slog.Handler { return h }
func (h *recordingHandler) WithGroup(name string) slog.Handler       { return h }

func TestCaptureHandlerCapturesErrorAndAboveOnly(t *testing.T) {
	var captured []slog.Record
	fake := func(_ context.Context, rec slog.Record, _ []slog.Attr) { captured = append(captured, rec) }
	next := &recordingHandler{}
	log := slog.New(&captureHandler{next: next, capture: fake, level: slog.LevelError})

	log.Info("info msg")
	log.Warn("warn msg")
	log.Error("boom")

	require.Len(t, captured, 1, "only Error+ records are captured as Issues")
	assert.Equal(t, "boom", captured[0].Message)
	assert.Len(t, next.records, 3, "every record is still delegated to next (the logs handler)")
}

func TestCaptureHandlerForwardsAccumulatedAttrs(t *testing.T) {
	var gotAttrs []slog.Attr
	fake := func(_ context.Context, _ slog.Record, attrs []slog.Attr) { gotAttrs = attrs }
	h := (&captureHandler{next: &recordingHandler{}, capture: fake, level: slog.LevelError}).
		WithAttrs([]slog.Attr{slog.String("svc", "auth")})
	slog.New(h).Error("boom")

	require.Len(t, gotAttrs, 1, "WithAttrs values must reach the capturer for Issue context")
	assert.Equal(t, "svc", gotAttrs[0].Key)
}

func TestCaptureHandlerEnabledBelowThreshold(t *testing.T) {
	// Even when next reports disabled, Error+ must stay enabled so Issues are captured.
	disabled := disabledHandler{}
	h := &captureHandler{next: disabled, capture: func(context.Context, slog.Record, []slog.Attr) {}, level: slog.LevelError}
	assert.True(t, h.Enabled(context.Background(), slog.LevelError))
	assert.False(t, h.Enabled(context.Background(), slog.LevelInfo))
}

func TestErrorFromRecordWrapsErrorAttr(t *testing.T) {
	sentinel := errors.New("db down")
	rec := slog.NewRecord(time.Time{}, slog.LevelError, "query failed", 0)
	rec.AddAttrs(slog.Any("error", sentinel))

	err := errorFromRecord(rec)
	assert.ErrorIs(t, err, sentinel, "original error must be preserved for grouping/stacktrace")
	assert.Contains(t, err.Error(), "query failed", "record message must be included")
}

func TestErrorFromRecordFallsBackToMessage(t *testing.T) {
	rec := slog.NewRecord(time.Time{}, slog.LevelError, "no error attr", 0)
	err := errorFromRecord(rec)
	require.Error(t, err)
	assert.Equal(t, "no error attr", err.Error())
}

// mockTransport records events instead of sending them, so captureException can be tested
// against a local hub without network or touching the global Sentry hub.
type mockTransport struct{ events []*sentry.Event }

func (m *mockTransport) Flush(time.Duration) bool              { return true }
func (m *mockTransport) FlushWithContext(context.Context) bool { return true }
func (m *mockTransport) Configure(sentry.ClientOptions)        {}
func (m *mockTransport) SendEvent(e *sentry.Event)             { m.events = append(m.events, e) }
func (m *mockTransport) Close()                                {}

func TestCaptureExceptionSendsIssueWithLogContext(t *testing.T) {
	mock := &mockTransport{}
	client, err := sentry.NewClient(sentry.ClientOptions{
		Dsn:       "https://publicKey@o0.ingest.sentry.io/0",
		Transport: mock,
	})
	require.NoError(t, err)
	// Bind the client to a LOCAL hub passed via context — no global hub mutation.
	ctx := sentry.SetHubOnContext(context.Background(), sentry.NewHub(client, sentry.NewScope()))

	rec := slog.NewRecord(time.Time{}, slog.LevelError, "boom", 0)
	rec.AddAttrs(slog.String("req_attr", "r1"))
	captureException(ctx, rec, []slog.Attr{slog.String("svc", "auth")})

	require.Len(t, mock.events, 1, "captureException must send exactly one exception event")
	ev := mock.events[0]
	require.NotEmpty(t, ev.Exception, "event must carry an exception (Issue)")
	assert.Contains(t, ev.Exception[len(ev.Exception)-1].Value, "boom", "exception carries the record message")

	logCtx := ev.Contexts["log"]
	require.NotNil(t, logCtx, "accumulated + record attrs attach under the \"log\" context")
	assert.Equal(t, "auth", logCtx["svc"], "WithAttrs value attached")
	assert.Equal(t, "r1", logCtx["req_attr"], "record attr attached")
}
