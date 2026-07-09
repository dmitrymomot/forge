package idempotency_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dmitrymomot/forge/resilience/cache"
	"github.com/dmitrymomot/forge/web/idempotency"
)

func okJSON() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":1}`)
	})
}

func req(method, target, body, key string) *http.Request {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	if key != "" {
		r.Header.Set("Idempotency-Key", key)
	}
	return r
}

func TestReplaysStoredResponse(t *testing.T) {
	var calls int32
	h := idempotency.New(cache.NewMemoryStore())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":1}`)
	}))

	r1 := httptest.NewRecorder()
	h.ServeHTTP(r1, req("POST", "/orders", `{"x":1}`, "abc"))
	r2 := httptest.NewRecorder()
	h.ServeHTTP(r2, req("POST", "/orders", `{"x":1}`, "abc"))

	if calls != 1 {
		t.Fatalf("handler called %d times, want 1", calls)
	}
	if r2.Code != http.StatusCreated {
		t.Fatalf("replay status = %d", r2.Code)
	}
	if r2.Body.String() != `{"id":1}` {
		t.Fatalf("replay body = %q", r2.Body.String())
	}
	if r2.Header().Get("Content-Type") != "application/json" {
		t.Fatal("replay lost Content-Type header")
	}
}

func TestDifferentPayloadRejected(t *testing.T) {
	h := idempotency.New(cache.NewMemoryStore())(okJSON())
	h.ServeHTTP(httptest.NewRecorder(), req("POST", "/p", `{"a":1}`, "k"))
	r2 := httptest.NewRecorder()
	h.ServeHTTP(r2, req("POST", "/p", `{"a":2}`, "k"))
	if r2.Code != http.StatusUnprocessableEntity {
		t.Fatalf("got %d, want 422", r2.Code)
	}
}

func TestConcurrentInFlightConflict(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})
	h := idempotency.New(cache.NewMemoryStore())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		w.WriteHeader(http.StatusOK)
	}))

	go h.ServeHTTP(httptest.NewRecorder(), req("POST", "/p", `{}`, "k"))
	<-started
	r2 := httptest.NewRecorder()
	h.ServeHTTP(r2, req("POST", "/p", `{}`, "k"))
	if r2.Code != http.StatusConflict {
		t.Fatalf("in-flight got %d, want 409", r2.Code)
	}
	close(release)
}

func TestUnguardedMethodPassThrough(t *testing.T) {
	var calls int32
	h := idempotency.New(cache.NewMemoryStore())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK)
	}))
	for range 3 {
		h.ServeHTTP(httptest.NewRecorder(), req("GET", "/p", "", "k"))
	}
	if calls != 3 {
		t.Fatalf("GET must not be deduped, calls=%d", calls)
	}
}

func TestMissingKey(t *testing.T) {
	h := idempotency.New(cache.NewMemoryStore())(okJSON())
	r := httptest.NewRecorder()
	h.ServeHTTP(r, req("POST", "/p", `{}`, ""))
	if r.Code != http.StatusCreated {
		t.Fatalf("default missing-key got %d, want pass-through 201", r.Code)
	}

	h2 := idempotency.New(cache.NewMemoryStore(), idempotency.WithRequireKey())(okJSON())
	r2 := httptest.NewRecorder()
	h2.ServeHTTP(r2, req("POST", "/p", `{}`, ""))
	if r2.Code != http.StatusBadRequest {
		t.Fatalf("required missing-key got %d, want 400", r2.Code)
	}
}

func TestServerErrorReleasesClaim(t *testing.T) {
	var calls int32
	h := idempotency.New(cache.NewMemoryStore())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	r1 := httptest.NewRecorder()
	h.ServeHTTP(r1, req("POST", "/p", `{}`, "k"))
	r2 := httptest.NewRecorder()
	h.ServeHTTP(r2, req("POST", "/p", `{}`, "k"))

	if r1.Code != http.StatusInternalServerError {
		t.Fatalf("first = %d", r1.Code)
	}
	if r2.Code != http.StatusOK {
		t.Fatalf("retry after 5xx should re-run, got %d", r2.Code)
	}
	if calls != 2 {
		t.Fatalf("calls=%d, want 2", calls)
	}
}

func TestSetCookieNotReplayed(t *testing.T) {
	h := idempotency.New(cache.NewMemoryStore())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "sid", Value: "secret"})
		w.WriteHeader(http.StatusOK)
	}))
	r1 := httptest.NewRecorder()
	h.ServeHTTP(r1, req("POST", "/p", `{}`, "k"))
	if r1.Header().Get("Set-Cookie") == "" {
		t.Fatal("first request should carry Set-Cookie")
	}
	r2 := httptest.NewRecorder()
	h.ServeHTTP(r2, req("POST", "/p", `{}`, "k"))
	if r2.Header().Get("Set-Cookie") != "" {
		t.Fatal("replayed response must not carry Set-Cookie")
	}
}

func TestOversizeRequestRejected(t *testing.T) {
	h := idempotency.New(cache.NewMemoryStore(), idempotency.WithMaxBodySize(1024))(okJSON())
	r := httptest.NewRecorder()
	h.ServeHTTP(r, req("POST", "/p", strings.Repeat("a", 2048), "k"))
	if r.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("got %d, want 413", r.Code)
	}
}

func TestOversizeResponseNotCached(t *testing.T) {
	var calls int32
	body := strings.Repeat("b", 2048)
	h := idempotency.New(cache.NewMemoryStore(), idempotency.WithMaxBodySize(1024))(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, body)
	}))
	r1 := httptest.NewRecorder()
	h.ServeHTTP(r1, req("POST", "/p", `{}`, "k"))
	if r1.Body.String() != body {
		t.Fatal("client must still receive the full oversize body")
	}
	h.ServeHTTP(httptest.NewRecorder(), req("POST", "/p", `{}`, "k"))
	if calls != 2 {
		t.Fatalf("oversize response should not be cached; calls=%d, want 2", calls)
	}
}
