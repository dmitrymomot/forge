package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type ctxKey struct{}

func reqIDExtractor(ctx context.Context) (slog.Attr, bool) {
	if v, ok := ctx.Value(ctxKey{}).(string); ok && v != "" {
		return slog.String("request_id", v), true
	}
	return slog.Attr{}, false
}

func decodeJSON(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	return m
}

func TestContextHandlerInjectsAtTopLevel(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(newContextHandler(slog.NewJSONHandler(&buf, nil), reqIDExtractor))
	ctx := context.WithValue(context.Background(), ctxKey{}, "abc-123")
	log.InfoContext(ctx, "hello")
	assert.Equal(t, "abc-123", decodeJSON(t, buf.Bytes())["request_id"])
}

func TestContextHandlerSkipsWhenExtractorReturnsFalse(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(newContextHandler(slog.NewJSONHandler(&buf, nil), reqIDExtractor))
	log.InfoContext(context.Background(), "hello") // no ctx value
	_, present := decodeJSON(t, buf.Bytes())["request_id"]
	assert.False(t, present)
}

func TestContextHandlerTopLevelUnderGroup(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(newContextHandler(slog.NewJSONHandler(&buf, nil), reqIDExtractor)).WithGroup("g")
	ctx := context.WithValue(context.Background(), ctxKey{}, "abc-123")
	log.InfoContext(ctx, "hello", slog.String("k", "v"))
	m := decodeJSON(t, buf.Bytes())
	assert.Equal(t, "abc-123", m["request_id"], "extracted attr must be top-level, not in group g")
	group, ok := m["g"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "v", group["k"])
}

func TestNewContextHandlerFiltersNilExtractors(t *testing.T) {
	h := newContextHandler(slog.NewJSONHandler(&bytes.Buffer{}, nil), nil, reqIDExtractor, nil)
	assert.Len(t, h.extractors, 1)
}

func TestContextHandlerConcurrentUse(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(newContextHandler(slog.NewJSONHandler(&buf, nil), reqIDExtractor))
	ctx := context.WithValue(context.Background(), ctxKey{}, "x")
	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() { log.InfoContext(ctx, "race") })
	}
	wg.Wait() // -race must stay clean
}
