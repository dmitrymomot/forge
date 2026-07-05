package logger_test

import (
	"log/slog"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/ops/logger"
)

func TestRecorder_CapturesLevelAndMessage(t *testing.T) {
	log, rec := logger.NewRecorder()
	log.Info("hello")
	log.Error("boom")
	assert.Equal(t, 2, rec.Len())
	assert.True(t, rec.Contains(slog.LevelInfo, "hello"))
	assert.True(t, rec.Contains(slog.LevelError, "boom"))
	assert.False(t, rec.Contains(slog.LevelWarn, "hello"))
}

func TestRecorder_FlattensGroupedAttrs(t *testing.T) {
	log, rec := logger.NewRecorder()
	log.Info("req", slog.Group("http", slog.Int("status", 200)))
	recs := rec.Records()
	require.Len(t, recs, 1)
	assert.Equal(t, int64(200), recs[0].Attrs["http.status"])
}

func TestRecorder_WithGroupAndWithAttrs(t *testing.T) {
	log, rec := logger.NewRecorder()
	log.With(slog.String("svc", "api")).WithGroup("db").Info("query", slog.Int("rows", 3))
	recs := rec.Records()
	require.Len(t, recs, 1)
	assert.Equal(t, "api", recs[0].Attrs["svc"])
	assert.Equal(t, int64(3), recs[0].Attrs["db.rows"])
}

func TestRecorder_Reset(t *testing.T) {
	log, rec := logger.NewRecorder()
	log.Info("x")
	rec.Reset()
	assert.Equal(t, 0, rec.Len())
	assert.Empty(t, rec.Records())
}

func TestRecorder_ConcurrentSafe(t *testing.T) {
	log, rec := logger.NewRecorder()
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Go(func() {
			log.Info("m", slog.Int("i", i))
		})
	}
	wg.Wait()
	assert.Equal(t, 50, rec.Len())
}
