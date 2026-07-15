package logger_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/ops/logger"
)

// closeCtx returns a context that bounds a drain wait in tests.
func closeCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestAsyncDeliversAllRecordsInOrder(t *testing.T) {
	var buf bytes.Buffer
	log, closeLog, err := logger.NewAsync(
		logger.WithOutput(&buf),
		logger.WithFormat(logger.FormatJSON),
	)
	require.NoError(t, err)

	for i := range 100 {
		log.Info("msg", "i", i)
	}
	require.NoError(t, closeLog(closeCtx(t)))

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.Len(t, lines, 100)
	for i, line := range lines {
		assert.Contains(t, line, fmt.Sprintf(`"i":%d`, i))
	}
}

func TestAsyncPreBoundAttrsAndGroups(t *testing.T) {
	rl, rec := logger.NewRecorder()
	log, closeLog, err := logger.NewAsync(
		logger.WithOutput(io.Discard),
		logger.WithHandler(rl.Handler()),
	)
	require.NoError(t, err)

	log.With("env", "prod").WithGroup("http").Info("done", "status", 200)
	require.NoError(t, closeLog(closeCtx(t)))

	recs := rec.Records()
	require.Len(t, recs, 1)
	assert.Equal(t, "done", recs[0].Message)
	assert.Equal(t, "prod", recs[0].Attrs["env"])
	assert.Equal(t, int64(200), recs[0].Attrs["http.status"])
}

func TestAsyncBelowLevelProducesNoOutput(t *testing.T) {
	var buf bytes.Buffer
	log, closeLog, err := logger.NewAsync(
		logger.WithOutput(&buf),
		logger.WithLevel(slog.LevelWarn),
	)
	require.NoError(t, err)

	log.Info("skip")
	require.NoError(t, closeLog(closeCtx(t)))
	assert.Empty(t, buf.String())
}

func TestAsyncConstructionErrors(t *testing.T) {
	log, closeLog, err := logger.NewAsync(logger.WithOutput(nil))
	require.Error(t, err)
	assert.ErrorIs(t, err, logger.ErrInvalidConfig)
	assert.Nil(t, log)
	assert.Nil(t, closeLog)
}

func TestAsyncConcurrentProducers(t *testing.T) {
	var buf bytes.Buffer
	log, closeLog, err := logger.NewAsync(logger.WithOutput(&buf))
	require.NoError(t, err)

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for i := range 200 {
				log.Info("concurrent", "i", i)
			}
		})
	}
	wg.Wait()
	require.NoError(t, closeLog(closeCtx(t)))
	// 1600 records < default 8192 buffer, so none can drop even if the worker never ran.
	assert.Equal(t, 1600, strings.Count(buf.String(), "concurrent"))
}

// gatedWriter blocks every Write until open() is called; entered signals each Write's
// arrival so tests can deterministically wait for the worker to be inside a Write.
type gatedWriter struct {
	gate    chan struct{}
	entered chan struct{}
	mu      sync.Mutex
	buf     bytes.Buffer
}

func newGatedWriter() *gatedWriter {
	return &gatedWriter{gate: make(chan struct{}), entered: make(chan struct{}, 64)}
}

func (w *gatedWriter) Write(p []byte) (int, error) {
	w.entered <- struct{}{}
	<-w.gate
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func (w *gatedWriter) open() { close(w.gate) }

func (w *gatedWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

func TestAsyncDropsOnFullBufferAndReportsTally(t *testing.T) {
	w := newGatedWriter()
	log, closeLog, err := logger.NewAsync(
		logger.WithOutput(w),
		logger.WithAsyncBufferSize(1),
	)
	require.NoError(t, err)

	log.Info("first")  // worker dequeues it and blocks inside Write
	<-w.entered        // worker is now committed to "first"; the queue is empty
	log.Info("second") // fills the single buffer slot
	log.Info("third")  // dropped
	log.Info("fourth") // dropped

	w.open()
	require.NoError(t, closeLog(closeCtx(t)))

	out := w.String()
	assert.Contains(t, out, "first")
	assert.Contains(t, out, "second")
	assert.NotContains(t, out, "third")
	assert.NotContains(t, out, "fourth")
	assert.Contains(t, out, "logger: dropped log records")
	assert.Contains(t, out, "dropped=2")
}

func TestAsyncDropNeverBlocksCaller(t *testing.T) {
	w := newGatedWriter() // never opened during logging: worker wedged on the first record
	log, closeLog, err := logger.NewAsync(
		logger.WithOutput(w),
		logger.WithAsyncBufferSize(1),
	)
	require.NoError(t, err)

	start := time.Now()
	for i := range 10_000 {
		log.Info("flood", "i", i)
	}
	elapsed := time.Since(start)
	assert.Less(t, elapsed, 2*time.Second, "log calls must not block on a wedged sink")

	w.open()
	require.NoError(t, closeLog(closeCtx(t)))
}

type asyncCtxKey struct{}

func TestAsyncCloseHonorsContextAndIsIdempotent(t *testing.T) {
	w := newGatedWriter()
	log, closeLog, err := logger.NewAsync(logger.WithOutput(w))
	require.NoError(t, err)

	log.Info("stuck")
	<-w.entered // worker blocked inside Write

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	assert.ErrorIs(t, closeLog(ctx), context.DeadlineExceeded)

	w.open()                                  // release the worker; the background drain completes
	require.NoError(t, closeLog(closeCtx(t))) // second call waits for the same drain
	assert.Contains(t, w.String(), "stuck")
}

func TestAsyncLogAfterCloseIsSilentNoOp(t *testing.T) {
	var buf bytes.Buffer
	log, closeLog, err := logger.NewAsync(logger.WithOutput(&buf))
	require.NoError(t, err)
	require.NoError(t, closeLog(closeCtx(t)))

	log.Info("after close") // must not panic, block, or write
	assert.NotContains(t, buf.String(), "after close")
}

func TestAsyncCloseConcurrentWithLogging(t *testing.T) {
	log, closeLog, err := logger.NewAsync(logger.WithOutput(io.Discard))
	require.NoError(t, err)

	var wg sync.WaitGroup
	wg.Go(func() {
		for i := range 1000 {
			log.Info("racing", "i", i)
		}
	})
	require.NoError(t, closeLog(closeCtx(t)))
	wg.Wait() // no panic, no race (verified under -race)
}

// ctxAwareHandler writes the record message to sink only if the context is not canceled at
// Handle time — so a worker that received a live (i.e. raw, still-cancelable) ctx would skip
// the write once the caller cancels it, while WithoutCancel lets it through regardless.
type ctxAwareHandler struct {
	mu   *sync.Mutex
	sink *strings.Builder
}

func (h ctxAwareHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h ctxAwareHandler) Handle(ctx context.Context, rec slog.Record) error {
	if ctx.Err() != nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sink.WriteString(rec.Message)
	return nil
}

func (h ctxAwareHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h ctxAwareHandler) WithGroup(string) slog.Handler      { return h }

// TestAsyncDropTallyReachesErrorGatedDestinations proves the drop report stays visible even
// when every destination is gated above Warn — a dropped-records warning bypasses per-
// destination level gating because it is a system-health signal.
func TestAsyncDropTallyReachesErrorGatedDestinations(t *testing.T) {
	w := newGatedWriter()
	rl, rec := logger.NewRecorder()
	log, closeLog, err := logger.NewAsync(
		logger.WithOutput(w),
		logger.WithLevel(slog.LevelError),                        // primary gated at error
		logger.WithLeveledHandler(slog.LevelError, rl.Handler()), // extra gated at error
		logger.WithAsyncBufferSize(1),
	)
	require.NoError(t, err)

	log.Error("first")  // worker dequeues it and blocks inside Write
	<-w.entered         // worker committed to "first"; the queue is empty
	log.Error("second") // fills the single buffer slot
	log.Error("third")  // dropped
	log.Error("fourth") // dropped

	w.open()
	require.NoError(t, closeLog(closeCtx(t)))

	// Primary (text, error-gated) received the Warn drop report directly.
	assert.Contains(t, w.String(), "logger: dropped log records")
	assert.Contains(t, w.String(), "dropped=2")
	// The error-gated extra destination received it too — not suppressed by its min level.
	var found bool
	for _, r := range rec.Records() {
		if r.Message == "logger: dropped log records" {
			found = true
			assert.Equal(t, int64(2), r.Attrs["dropped"])
		}
	}
	assert.True(t, found, "error-gated extra destination must still receive the drop report")
}

func TestAsyncExtractorValuesCapturedAtCallTime(t *testing.T) {
	w := newGatedWriter()
	extractor := func(ctx context.Context) (slog.Attr, bool) {
		if v, ok := ctx.Value(asyncCtxKey{}).(string); ok {
			return slog.String("request_id", v), true
		}
		return slog.Attr{}, false
	}
	var sinkMu sync.Mutex
	var sink strings.Builder
	log, closeLog, err := logger.NewAsync(
		logger.WithOutput(w),
		logger.WithHandler(ctxAwareHandler{mu: &sinkMu, sink: &sink}),
		logger.WithContextExtractors(extractor),
	)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), asyncCtxKey{}, "abc-123"))
	log.InfoContext(ctx, "req done")
	cancel() // the request ends before the worker ever writes

	w.open()
	require.NoError(t, closeLog(closeCtx(t)))
	// Extraction ran on the caller's goroutine at log time, and WithoutCancel kept the
	// canceled request from suppressing the write.
	assert.Contains(t, w.String(), "request_id=abc-123")

	// ctxAwareHandler only writes when the ctx it receives is not canceled. cancel() above
	// fires before the worker ever calls Handle, so a naive implementation that forwarded the
	// raw (still-cancelable) ctx into the worker would leave sink empty here; WithoutCancel is
	// what lets this write through, proving async.go actually strips cancellation.
	sinkMu.Lock()
	defer sinkMu.Unlock()
	assert.Contains(t, sink.String(), "req done")
}
