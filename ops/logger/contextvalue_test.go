package logger_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/ops/logger"
)

type valueKey struct{}

func logWithExtractor(t *testing.T, ctx context.Context, ex logger.ContextExtractor) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	log, err := logger.New(
		logger.WithOutput(&buf),
		logger.WithFormat(logger.FormatJSON),
		logger.WithContextExtractors(ex),
	)
	require.NoError(t, err)
	log.InfoContext(ctx, "hello")

	var m map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &m))
	return m
}

func TestContextValue_ReportsStoredValue(t *testing.T) {
	ctx := context.WithValue(t.Context(), valueKey{}, "abc-123")
	m := logWithExtractor(t, ctx, logger.ContextValue[string](valueKey{}, "request_id"))
	assert.Equal(t, "abc-123", m["request_id"])
}

func TestContextValue_SkipsMissingValue(t *testing.T) {
	m := logWithExtractor(t, t.Context(), logger.ContextValue[string](valueKey{}, "request_id"))
	assert.NotContains(t, m, "request_id")
}

// TestContextValue_SkipsWrongType covers the case a plain type assertion would panic on:
// another package storing a different type under the same key.
func TestContextValue_SkipsWrongType(t *testing.T) {
	ctx := context.WithValue(t.Context(), valueKey{}, 42)
	m := logWithExtractor(t, ctx, logger.ContextValue[string](valueKey{}, "request_id"))
	assert.NotContains(t, m, "request_id")
}

func TestContextValue_CarriesNonStringTypes(t *testing.T) {
	type tenant struct {
		ID string `json:"id"`
	}
	ctx := context.WithValue(t.Context(), valueKey{}, tenant{ID: "acme"})
	m := logWithExtractor(t, ctx, logger.ContextValue[tenant](valueKey{}, "tenant"))
	assert.Equal(t, map[string]any{"id": "acme"}, m["tenant"])
}

func TestContextValue_ReportsZeroValueWhenStored(t *testing.T) {
	ctx := context.WithValue(t.Context(), valueKey{}, "")
	m := logWithExtractor(t, ctx, logger.ContextValue[string](valueKey{}, "request_id"))
	assert.Equal(t, "", m["request_id"])
	assert.Contains(t, m, "request_id")
}

func BenchmarkContextValue(b *testing.B) {
	ex := logger.ContextValue[string](valueKey{}, "request_id")
	ctx := context.WithValue(context.Background(), valueKey{}, "abc-123")
	b.ReportAllocs()
	for b.Loop() {
		if _, ok := ex(ctx); !ok {
			b.Fatal("miss")
		}
	}
}
