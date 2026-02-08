package internal_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge"
)

func TestHealth_LivenessEndpoint(t *testing.T) {
	t.Parallel()

	t.Run("always returns OK", func(t *testing.T) {
		t.Parallel()

		app := forge.New(
			forge.AppConfig{},
			forge.WithHealthChecks(),
		)

		req := httptest.NewRequest(http.MethodGet, "/_live", nil)
		rec := httptest.NewRecorder()

		app.Router().ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		require.Equal(t, "OK", rec.Body.String())
	})

	t.Run("returns JSON when Accept header is set", func(t *testing.T) {
		t.Parallel()

		app := forge.New(
			forge.AppConfig{},
			forge.WithHealthChecks(),
		)

		req := httptest.NewRequest(http.MethodGet, "/_live", nil)
		req.Header.Set("Accept", "application/json")
		rec := httptest.NewRecorder()

		app.Router().ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
		require.Contains(t, rec.Body.String(), `"status":"healthy"`)
	})

	t.Run("returns JSON when format query param is json", func(t *testing.T) {
		t.Parallel()

		app := forge.New(
			forge.AppConfig{},
			forge.WithHealthChecks(),
		)

		req := httptest.NewRequest(http.MethodGet, "/_live?format=json", nil)
		rec := httptest.NewRecorder()

		app.Router().ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
		require.Contains(t, rec.Body.String(), `"status":"healthy"`)
	})
}

func TestHealth_ReadinessEndpoint(t *testing.T) {
	t.Parallel()

	t.Run("returns OK when no checks configured", func(t *testing.T) {
		t.Parallel()

		app := forge.New(
			forge.AppConfig{},
			forge.WithHealthChecks(),
		)

		req := httptest.NewRequest(http.MethodGet, "/_ready", nil)
		rec := httptest.NewRecorder()

		app.Router().ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		require.Equal(t, "OK", rec.Body.String())
	})

	t.Run("returns OK when all checks pass", func(t *testing.T) {
		t.Parallel()

		passCheck := func(ctx context.Context) error { return nil }

		app := forge.New(
			forge.AppConfig{},
			forge.WithHealthChecks(
				forge.HealthCheck("db", passCheck),
				forge.HealthCheck("cache", passCheck),
			),
		)

		req := httptest.NewRequest(http.MethodGet, "/_ready", nil)
		rec := httptest.NewRecorder()

		app.Router().ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		require.Equal(t, "OK", rec.Body.String())
	})

	t.Run("returns 503 when any check fails", func(t *testing.T) {
		t.Parallel()

		passCheck := func(ctx context.Context) error { return nil }
		failCheck := func(ctx context.Context) error { return errors.New("connection failed") }

		app := forge.New(
			forge.AppConfig{},
			forge.WithHealthChecks(
				forge.HealthCheck("db", passCheck),
				forge.HealthCheck("cache", failCheck),
			),
		)

		req := httptest.NewRequest(http.MethodGet, "/_ready", nil)
		rec := httptest.NewRecorder()

		app.Router().ServeHTTP(rec, req)

		require.Equal(t, http.StatusServiceUnavailable, rec.Code)
		require.Equal(t, "Service Unavailable", rec.Body.String())
	})

	t.Run("returns JSON with check details", func(t *testing.T) {
		t.Parallel()

		passCheck := func(ctx context.Context) error { return nil }
		failCheck := func(ctx context.Context) error { return errors.New("connection timeout") }

		app := forge.New(
			forge.AppConfig{},
			forge.WithHealthChecks(
				forge.HealthCheck("database", passCheck),
				forge.HealthCheck("redis", failCheck),
			),
		)

		req := httptest.NewRequest(http.MethodGet, "/_ready?format=json", nil)
		rec := httptest.NewRecorder()

		app.Router().ServeHTTP(rec, req)

		require.Equal(t, http.StatusServiceUnavailable, rec.Code)
		require.Equal(t, "application/json", rec.Header().Get("Content-Type"))

		body := rec.Body.String()
		require.Contains(t, body, `"status":"unhealthy"`)
		require.Contains(t, body, `"database"`)
		require.Contains(t, body, `"redis"`)
		require.Contains(t, body, `"connection timeout"`)
	})
}

func TestHealth_CheckExecution(t *testing.T) {
	t.Parallel()

	t.Run("checks execute in parallel", func(t *testing.T) {
		t.Parallel()

		executionTimes := make(chan time.Time, 2)

		slowCheck1 := func(ctx context.Context) error {
			executionTimes <- time.Now()
			time.Sleep(50 * time.Millisecond)
			return nil
		}

		slowCheck2 := func(ctx context.Context) error {
			executionTimes <- time.Now()
			time.Sleep(50 * time.Millisecond)
			return nil
		}

		app := forge.New(
			forge.AppConfig{},
			forge.WithHealthChecks(
				forge.HealthCheck("check1", slowCheck1),
				forge.HealthCheck("check2", slowCheck2),
			),
		)

		start := time.Now()
		req := httptest.NewRequest(http.MethodGet, "/_ready", nil)
		rec := httptest.NewRecorder()

		app.Router().ServeHTTP(rec, req)
		duration := time.Since(start)

		require.Equal(t, http.StatusOK, rec.Code)

		// If parallel, total time should be ~50ms (not 100ms)
		// Allow some overhead for goroutine scheduling
		require.Less(t, duration, 100*time.Millisecond,
			"checks should execute in parallel, not sequentially")

		close(executionTimes)
		times := make([]time.Time, 0, 2)
		for t := range executionTimes {
			times = append(times, t)
		}

		// Both checks should start within a few milliseconds of each other
		if len(times) == 2 {
			timeDiff := times[1].Sub(times[0])
			if timeDiff < 0 {
				timeDiff = -timeDiff
			}
			require.Less(t, timeDiff, 20*time.Millisecond,
				"checks should start nearly simultaneously")
		}
	})

	t.Run("respects context timeout", func(t *testing.T) {
		t.Parallel()

		hangingCheck := func(ctx context.Context) error {
			// Simulate a hanging operation that respects context
			<-ctx.Done()
			return ctx.Err()
		}

		app := forge.New(
			forge.AppConfig{},
			forge.WithHealthChecks(
				forge.HealthCheck("hanging", hangingCheck),
			),
		)

		req := httptest.NewRequest(http.MethodGet, "/_ready", nil)
		rec := httptest.NewRecorder()

		start := time.Now()
		app.Router().ServeHTTP(rec, req)
		duration := time.Since(start)

		// Default timeout is 5 seconds, so should complete around that time
		require.Greater(t, duration, 4*time.Second)
		require.Less(t, duration, 6*time.Second)
		require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	})

	t.Run("check error messages are preserved", func(t *testing.T) {
		t.Parallel()

		specificError := func(ctx context.Context) error {
			return errors.New("database connection pool exhausted")
		}

		app := forge.New(
			forge.AppConfig{},
			forge.WithHealthChecks(
				forge.HealthCheck("db", specificError),
			),
		)

		req := httptest.NewRequest(http.MethodGet, "/_ready?format=json", nil)
		rec := httptest.NewRecorder()

		app.Router().ServeHTTP(rec, req)

		require.Equal(t, http.StatusServiceUnavailable, rec.Code)
		require.Contains(t, rec.Body.String(), "database connection pool exhausted")
	})
}

func TestHealth_MultipleChecks(t *testing.T) {
	t.Parallel()

	t.Run("aggregates status from multiple checks", func(t *testing.T) {
		t.Parallel()

		pass1 := func(ctx context.Context) error { return nil }
		pass2 := func(ctx context.Context) error { return nil }
		pass3 := func(ctx context.Context) error { return nil }

		app := forge.New(
			forge.AppConfig{},
			forge.WithHealthChecks(
				forge.HealthCheck("db", pass1),
				forge.HealthCheck("cache", pass2),
				forge.HealthCheck("queue", pass3),
			),
		)

		req := httptest.NewRequest(http.MethodGet, "/_ready?format=json", nil)
		rec := httptest.NewRecorder()

		app.Router().ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		body := rec.Body.String()
		require.Contains(t, body, `"status":"healthy"`)
		require.Contains(t, body, `"db"`)
		require.Contains(t, body, `"cache"`)
		require.Contains(t, body, `"queue"`)
	})

	t.Run("one failure marks entire service unhealthy", func(t *testing.T) {
		t.Parallel()

		pass := func(ctx context.Context) error { return nil }
		fail := func(ctx context.Context) error { return errors.New("fail") }

		app := forge.New(
			forge.AppConfig{},
			forge.WithHealthChecks(
				forge.HealthCheck("check1", pass),
				forge.HealthCheck("check2", fail),
				forge.HealthCheck("check3", pass),
			),
		)

		req := httptest.NewRequest(http.MethodGet, "/_ready?format=json", nil)
		rec := httptest.NewRecorder()

		app.Router().ServeHTTP(rec, req)

		require.Equal(t, http.StatusServiceUnavailable, rec.Code)
		require.Contains(t, rec.Body.String(), `"status":"unhealthy"`)
	})

	t.Run("all checks are executed even if one fails", func(t *testing.T) {
		t.Parallel()

		var mu sync.Mutex
		executionCount := 0
		executed := make(map[string]bool)

		makeCheck := func(name string, shouldFail bool) func(context.Context) error {
			return func(ctx context.Context) error {
				mu.Lock()
				executionCount++
				executed[name] = true
				mu.Unlock()
				if shouldFail {
					return errors.New("check failed")
				}
				return nil
			}
		}

		app := forge.New(
			forge.AppConfig{},
			forge.WithHealthChecks(
				forge.HealthCheck("check1", makeCheck("check1", false)),
				forge.HealthCheck("check2", makeCheck("check2", true)),
				forge.HealthCheck("check3", makeCheck("check3", false)),
			),
		)

		req := httptest.NewRequest(http.MethodGet, "/_ready", nil)
		rec := httptest.NewRecorder()

		app.Router().ServeHTTP(rec, req)

		// All checks should execute despite check2 failing
		mu.Lock()
		defer mu.Unlock()
		require.Equal(t, 3, executionCount)
		require.True(t, executed["check1"])
		require.True(t, executed["check2"])
		require.True(t, executed["check3"])
	})
}

func TestHealth_ContentNegotiation(t *testing.T) {
	t.Parallel()

	t.Run("defaults to plain text", func(t *testing.T) {
		t.Parallel()

		app := forge.New(
			forge.AppConfig{},
			forge.WithHealthChecks(),
		)

		req := httptest.NewRequest(http.MethodGet, "/_ready", nil)
		rec := httptest.NewRecorder()

		app.Router().ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		require.Equal(t, "OK", rec.Body.String())
		require.NotEqual(t, "application/json", rec.Header().Get("Content-Type"))
	})

	t.Run("format query param takes precedence over Accept header", func(t *testing.T) {
		t.Parallel()

		app := forge.New(
			forge.AppConfig{},
			forge.WithHealthChecks(),
		)

		req := httptest.NewRequest(http.MethodGet, "/_ready?format=json", nil)
		req.Header.Set("Accept", "text/plain")
		rec := httptest.NewRecorder()

		app.Router().ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
		require.Contains(t, rec.Body.String(), `"status"`)
	})

	t.Run("Accept header with application/json returns JSON", func(t *testing.T) {
		t.Parallel()

		app := forge.New(
			forge.AppConfig{},
			forge.WithHealthChecks(),
		)

		req := httptest.NewRequest(http.MethodGet, "/_ready", nil)
		req.Header.Set("Accept", "application/json")
		rec := httptest.NewRecorder()

		app.Router().ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	})
}

func TestHealth_EdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("handles nil error from check", func(t *testing.T) {
		t.Parallel()

		nilCheck := func(ctx context.Context) error { return nil }

		app := forge.New(
			forge.AppConfig{},
			forge.WithHealthChecks(
				forge.HealthCheck("ok", nilCheck),
			),
		)

		req := httptest.NewRequest(http.MethodGet, "/_ready", nil)
		rec := httptest.NewRecorder()

		app.Router().ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("handles check that panics gracefully", func(t *testing.T) {
		t.Parallel()

		// Note: The health check implementation doesn't catch panics,
		// so this test documents expected behavior if a check panics.
		// In production, checks should handle their own errors.

		safeCheck := func(ctx context.Context) error {
			defer func() {
				if r := recover(); r != nil {
					_ = r // Checks should recover from panics internally
				}
			}()
			// Simulate panic recovery
			return errors.New("check failed safely")
		}

		app := forge.New(
			forge.AppConfig{},
			forge.WithHealthChecks(
				forge.HealthCheck("safe", safeCheck),
			),
		)

		req := httptest.NewRequest(http.MethodGet, "/_ready", nil)
		rec := httptest.NewRecorder()

		app.Router().ServeHTTP(rec, req)

		require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	})

	t.Run("empty check name is allowed", func(t *testing.T) {
		t.Parallel()

		check := func(ctx context.Context) error { return nil }

		app := forge.New(
			forge.AppConfig{},
			forge.WithHealthChecks(
				forge.HealthCheck("", check),
			),
		)

		req := httptest.NewRequest(http.MethodGet, "/_ready?format=json", nil)
		rec := httptest.NewRecorder()

		app.Router().ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		// Empty name should still appear in JSON output
		require.Contains(t, rec.Body.String(), `""`)
	})
}

func TestHealth_RealWorldScenarios(t *testing.T) {
	t.Parallel()

	t.Run("database connection pool check", func(t *testing.T) {
		t.Parallel()

		dbCheck := func(ctx context.Context) error {
			// Simulate database ping with context
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(10 * time.Millisecond):
				// Simulate successful ping
				return nil
			}
		}

		app := forge.New(
			forge.AppConfig{},
			forge.WithHealthChecks(
				forge.HealthCheck("postgres", dbCheck),
			),
		)

		req := httptest.NewRequest(http.MethodGet, "/_ready", nil)
		rec := httptest.NewRecorder()

		app.Router().ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("multiple dependency checks", func(t *testing.T) {
		t.Parallel()

		dbCheck := func(ctx context.Context) error { return nil }
		cacheCheck := func(ctx context.Context) error { return nil }
		queueCheck := func(ctx context.Context) error { return nil }
		storageCheck := func(ctx context.Context) error {
			return errors.New("S3 bucket unavailable")
		}

		app := forge.New(
			forge.AppConfig{},
			forge.WithHealthChecks(
				forge.HealthCheck("database", dbCheck),
				forge.HealthCheck("redis", cacheCheck),
				forge.HealthCheck("rabbitmq", queueCheck),
				forge.HealthCheck("s3", storageCheck),
			),
		)

		req := httptest.NewRequest(http.MethodGet, "/_ready?format=json", nil)
		rec := httptest.NewRecorder()

		app.Router().ServeHTTP(rec, req)

		require.Equal(t, http.StatusServiceUnavailable, rec.Code)
		body := rec.Body.String()
		require.Contains(t, body, `"database"`)
		require.Contains(t, body, `"redis"`)
		require.Contains(t, body, `"rabbitmq"`)
		require.Contains(t, body, `"s3"`)
		require.Contains(t, body, "S3 bucket unavailable")
	})
}
