package idempotency_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
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

func TestNamespaceIsolatesKeys(t *testing.T) {
	store := cache.NewMemoryStore()
	var calls int32
	ns := func(r *http.Request) string { return r.Header.Get("X-Tenant") }
	h := idempotency.New(store, idempotency.WithNamespace(ns))(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusCreated)
	}))
	r1 := req("POST", "/p", `{"x":1}`, "k")
	r1.Header.Set("X-Tenant", "acme")
	r2 := req("POST", "/p", `{"x":1}`, "k")
	r2.Header.Set("X-Tenant", "globex")
	h.ServeHTTP(httptest.NewRecorder(), r1)
	h.ServeHTTP(httptest.NewRecorder(), r2)
	if calls != 2 {
		t.Fatalf("different tenants must not share keys; calls=%d, want 2", calls)
	}
	r3 := req("POST", "/p", `{"x":1}`, "k")
	r3.Header.Set("X-Tenant", "acme")
	h.ServeHTTP(httptest.NewRecorder(), r3)
	if calls != 2 {
		t.Fatalf("same tenant+key must replay; calls=%d, want 2", calls)
	}
}

func TestNamespaceJoinIsCollisionSafe(t *testing.T) {
	store := cache.NewMemoryStore()
	var calls int32
	ns := func(r *http.Request) string { return r.Header.Get("X-NS") }
	h := idempotency.New(store, idempotency.WithNamespace(ns))(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusCreated)
	}))
	// Under a naive ns+":"+key join these two collide to "a:b:c"; length-framing keeps them distinct.
	rA := req("POST", "/p", `{"x":1}`, "b:c")
	rA.Header.Set("X-NS", "a")
	rB := req("POST", "/p", `{"x":1}`, "c")
	rB.Header.Set("X-NS", "a:b")
	h.ServeHTTP(httptest.NewRecorder(), rA)
	h.ServeHTTP(httptest.NewRecorder(), rB)
	if calls != 2 {
		t.Fatalf("namespaces that collide under a naive join must stay distinct; calls=%d, want 2", calls)
	}
}

func TestFlushingResponseNotCached(t *testing.T) {
	var calls int32
	h := idempotency.New(cache.NewMemoryStore())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "part")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))
	h.ServeHTTP(httptest.NewRecorder(), req("POST", "/p", `{}`, "k"))
	h.ServeHTTP(httptest.NewRecorder(), req("POST", "/p", `{}`, "k"))
	if calls != 2 {
		t.Fatalf("flushed (streamed) response must not be cached; calls=%d, want 2", calls)
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

type failingStore struct{}

func (failingStore) Get(context.Context, string) ([]byte, error) {
	return nil, errors.New("store down")
}
func (failingStore) Set(context.Context, string, []byte, ...cache.SetOption) error {
	return errors.New("store down")
}
func (failingStore) Delete(context.Context, string) error { return errors.New("store down") }
func (failingStore) Has(context.Context, string) (bool, error) {
	return false, errors.New("store down")
}
func (failingStore) DeletePrefix(context.Context, string) error { return errors.New("store down") }
func (failingStore) Close() error                               { return nil }

func TestStoreUnavailableExecutesOnce(t *testing.T) {
	var calls int32
	h := idempotency.New(failingStore{})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusCreated)
	}))
	h.ServeHTTP(httptest.NewRecorder(), req("POST", "/p", `{}`, "k"))
	h.ServeHTTP(httptest.NewRecorder(), req("POST", "/p", `{}`, "k"))
	if calls != 2 {
		t.Fatalf("store down must execute each time (cannot guarantee idempotency); calls=%d, want 2", calls)
	}
}

func TestStoreUnavailableConcurrentDuplicatesBothExecute(t *testing.T) {
	var calls int32
	h := idempotency.New(failingStore{})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK)
	}))
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() { h.ServeHTTP(httptest.NewRecorder(), req("POST", "/p", `{}`, "k")) })
	}
	wg.Wait()
	if calls != 2 {
		t.Fatalf("store down: concurrent duplicates both execute; calls=%d, want 2", calls)
	}
}

func TestPanicReleasesClaim(t *testing.T) {
	store := cache.NewMemoryStore()
	var calls int32
	h := idempotency.New(store)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			panic("boom")
		}
		w.WriteHeader(http.StatusOK)
	}))
	func() {
		defer func() { _ = recover() }() // absorb the propagating panic
		h.ServeHTTP(httptest.NewRecorder(), req("POST", "/p", `{}`, "k"))
	}()
	r2 := httptest.NewRecorder()
	h.ServeHTTP(r2, req("POST", "/p", `{}`, "k"))
	if r2.Code != http.StatusOK {
		t.Fatalf("after panic-release, retry must re-execute: got %d, want 200", r2.Code)
	}
	if calls != 2 {
		t.Fatalf("calls=%d, want 2 (panic must release the claim)", calls)
	}
}

func BenchmarkReplay(b *testing.B) {
	store := cache.NewMemoryStore()
	h := idempotency.New(store)(okJSON())
	h.ServeHTTP(httptest.NewRecorder(), req("POST", "/p", `{"x":1}`, "k")) // prime

	payload := `{"x":1}`
	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		r := req("POST", "/p", payload, "k")
		h.ServeHTTP(httptest.NewRecorder(), r)
	}
}

func BenchmarkFirstCall(b *testing.B) {
	h := idempotency.New(cache.NewMemoryStore())(okJSON())
	b.ReportAllocs()
	for i := range b.N {
		// distinct key per iteration so each is a fresh first-call
		r := req("POST", "/p", `{"x":1}`, strconv.Itoa(i))
		h.ServeHTTP(httptest.NewRecorder(), r)
	}
}
