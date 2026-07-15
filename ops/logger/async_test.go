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
