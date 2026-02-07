package internal

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	defaultHealthTimeout = 5 * time.Second

	statusHealthy   = "healthy"
	statusUnhealthy = "unhealthy"
)

// CheckFunc is the standard health check function signature.
// This matches existing healthcheck closures in pg, redis, cqrs, and jobs packages.
type CheckFunc func(ctx context.Context) error

// healthChecks is a map of named health check functions.
type healthChecks map[string]CheckFunc

// healthResponse represents a health check response.
type healthResponse struct {
	Checks map[string]healthCheck `json:"checks,omitempty"`
	Status string                 `json:"status"`
}

// healthCheck represents the status of a single health check.
type healthCheck struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// livenessHandler returns an http.HandlerFunc that always responds OK.
func livenessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if wantsJSON(r) {
			writeHealthJSON(w, http.StatusOK, &healthResponse{Status: statusHealthy})
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}
}

// readinessHandler returns an http.HandlerFunc that runs all provided checks.
func readinessHandler(checks healthChecks) http.HandlerFunc {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	return func(w http.ResponseWriter, r *http.Request) {
		resp := runChecks(r.Context(), checks, defaultHealthTimeout, logger)

		status := http.StatusOK
		if resp.Status == statusUnhealthy {
			status = http.StatusServiceUnavailable
		}

		if wantsJSON(r) {
			writeHealthJSON(w, status, resp)
			return
		}

		w.WriteHeader(status)
		if resp.Status == statusHealthy {
			_, _ = w.Write([]byte("OK"))
		} else {
			_, _ = w.Write([]byte("Service Unavailable"))
		}
	}
}

// runChecks executes all checks in parallel and returns the aggregated result.
func runChecks(ctx context.Context, checks healthChecks, timeout time.Duration, logger *slog.Logger) *healthResponse {
	if len(checks) == 0 {
		return &healthResponse{Status: statusHealthy}
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		results  = make(map[string]healthCheck, len(checks))
		hasError bool
	)

	for name, check := range checks {
		wg.Add(1)
		go func(name string, check CheckFunc) {
			defer wg.Done()

			result := healthCheck{Status: statusHealthy}
			if err := check(ctx); err != nil {
				result.Status = statusUnhealthy
				result.Error = err.Error()
				logger.WarnContext(ctx, "health check failed",
					slog.String("check", name),
					slog.String("error", err.Error()),
				)
				mu.Lock()
				hasError = true
				mu.Unlock()
			}

			mu.Lock()
			results[name] = result
			mu.Unlock()
		}(name, check)
	}

	wg.Wait()

	status := statusHealthy
	if hasError {
		status = statusUnhealthy
	}

	return &healthResponse{
		Status: status,
		Checks: results,
	}
}

// wantsJSON checks if the client wants JSON response.
func wantsJSON(r *http.Request) bool {
	if r.URL.Query().Get("format") == "json" {
		return true
	}
	accept := r.Header.Get("Accept")
	return strings.Contains(accept, "application/json")
}

// writeHealthJSON writes a JSON response.
func writeHealthJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
