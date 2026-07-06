package timeout_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/web/middleware"
	"github.com/dmitrymomot/forge/web/timeout"
)

func newMW(t *testing.T, d time.Duration) middleware.Middleware {
	t.Helper()
	mw, err := timeout.New(timeout.WithConfig(timeout.Config{Timeout: d}))
	if err != nil {
		t.Fatal(err)
	}
	return mw
}

func TestSlowHandlerGets504(t *testing.T) {
	h := middleware.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done() // ctx-respecting slow handler
	}), newMW(t, 10*time.Millisecond))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want 504", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
		t.Fatalf("want problem+json, got %q", ct)
	}
}

func TestFastHandlerUntouched(t *testing.T) {
	h := middleware.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}), newMW(t, time.Second))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
		t.Fatalf("fast handler mangled: %d %q", rec.Code, rec.Body.String())
	}
}

func TestDeadlinePropagatedToContext(t *testing.T) {
	h := middleware.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := r.Context().Deadline(); !ok {
			t.Error("deadline missing from request context")
		}
	}), newMW(t, time.Second))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}

func TestAlreadyWrittenResponseLeftAlone(t *testing.T) {
	h := middleware.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, "partial")
		<-r.Context().Done()
	}), newMW(t, 10*time.Millisecond))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusAccepted || rec.Body.String() != "partial" {
		t.Fatalf("committed response must not be rewritten: %d %q", rec.Code, rec.Body.String())
	}
}

func TestInvalidConfig(t *testing.T) {
	if _, err := timeout.New(timeout.WithConfig(timeout.Config{Timeout: 0})); !errors.Is(err, timeout.ErrInvalidConfig) {
		t.Fatalf("zero timeout must be rejected, got %v", err)
	}
}

func TestDefaultConfig(t *testing.T) {
	if d := timeout.DefaultConfig().Timeout; d != 30*time.Second {
		t.Fatalf("default = %v, want 30s", d)
	}
}
