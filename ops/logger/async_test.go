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
