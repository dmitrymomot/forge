package health_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/ops/health"
)

func do(h http.Handler) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	return rec
}

func TestHandler_LivenessNoChecks(t *testing.T) {
	rec := do(health.Handler())
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestHandler_CriticalFailureIs503(t *testing.T) {
	rec := do(health.Handler(
		health.WithCheck("ok", func(context.Context) error { return nil }),
		health.WithCheck("db", func(context.Context) error { return errors.New("down") }),
	))
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)

	var rep health.Report
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &rep))
	assert.Equal(t, "unavailable", rep.Status)
}

func TestHandler_NonCriticalDegrades200(t *testing.T) {
	rec := do(health.Handler(
		health.WithCheck("cache", func(context.Context) error { return errors.New("down") }, health.NonCritical()),
	))
	assert.Equal(t, http.StatusOK, rec.Code)
	var rep health.Report
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &rep))
	assert.Equal(t, "degraded", rep.Status)
}

func TestHandler_TimeoutIsFailureNotHang(t *testing.T) {
	rec := do(health.Handler(
		health.WithCheck("slow", func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		}),
		health.WithTimeout(20*time.Millisecond),
	))
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestHandler_PanickingCheckIsContained(t *testing.T) {
	rec := do(health.Handler(
		health.WithCheck("panicky", func(context.Context) error { panic("boom") }),
	))
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)

	var rep health.Report
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &rep))
	require.Len(t, rep.Checks, 1)
	assert.False(t, rep.Checks[0].OK)
	assert.Contains(t, rep.Checks[0].Err, "panic")
}

func TestHandler_MixedCriticalAndNonCriticalFailureIsUnavailable(t *testing.T) {
	rec := do(health.Handler(
		health.WithCheck("db", func(context.Context) error { return errors.New("down") }),
		health.WithCheck("cache", func(context.Context) error { return errors.New("down") }, health.NonCritical()),
	))
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)

	var rep health.Report
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &rep))
	assert.Equal(t, "unavailable", rep.Status)
}
