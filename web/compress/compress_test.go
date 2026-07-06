package compress_test

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmitrymomot/forge/web/compress"
	"github.com/dmitrymomot/forge/web/middleware"
)

func newHandler(t *testing.T, h http.HandlerFunc, opts ...compress.Option) http.Handler {
	t.Helper()
	mw, err := compress.New(opts...)
	if err != nil {
		t.Fatal(err)
	}
	return middleware.Wrap(h, mw)
}

func gunzip(t *testing.T, b []byte) string {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func bigBody() string { return strings.Repeat("forge compresses text. ", 200) } // ~4.6 KB

func TestGzipLargeTextResponse(t *testing.T) {
	body := bigBody()
	h := newHandler(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, body)
	})
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Accept-Encoding", "gzip, deflate")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("Content-Encoding = %q", rec.Header().Get("Content-Encoding"))
	}
	if rec.Header().Get("Content-Length") != "" {
		t.Fatal("Content-Length must be stripped")
	}
	if !strings.Contains(rec.Header().Get("Vary"), "Accept-Encoding") {
		t.Fatal("Vary: Accept-Encoding missing")
	}
	if got := gunzip(t, rec.Body.Bytes()); got != body {
		t.Fatal("round-trip mismatch")
	}
}

func TestSmallResponseSkipped(t *testing.T) {
	h := newHandler(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "tiny")
	})
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Header().Get("Content-Encoding") != "" {
		t.Fatal("sub-MinSize body must not be compressed")
	}
	if rec.Body.String() != "tiny" {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestNoAcceptEncodingPassthrough(t *testing.T) {
	h := newHandler(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, bigBody())
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Header().Get("Content-Encoding") != "" {
		t.Fatal("no Accept-Encoding → no compression")
	}
}

func TestQZeroDisablesGzip(t *testing.T) {
	h := newHandler(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, bigBody())
	})
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Accept-Encoding", "gzip;q=0, deflate;q=0")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Header().Get("Content-Encoding") != "" {
		t.Fatal("q=0 must disable compression")
	}
}

func TestGzipPreferredOverDeflateOnTie(t *testing.T) {
	h := newHandler(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, bigBody())
	})
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Accept-Encoding", "deflate, gzip")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("tie must prefer gzip, got %q", got)
	}
}

func TestDisallowedContentTypeSkipped(t *testing.T) {
	h := newHandler(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(bytes.Repeat([]byte{0x89}, 4096))
	})
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Header().Get("Content-Encoding") != "" {
		t.Fatal("image/png must not be compressed")
	}
}

func TestAlreadyEncodedSkipped(t *testing.T) {
	h := newHandler(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Encoding", "br")
		_, _ = io.WriteString(w, bigBody())
	})
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if got := rec.Header().Get("Content-Encoding"); got != "br" {
		t.Fatalf("pre-encoded response must pass through, got %q", got)
	}
}

func TestRangeRequestSkipped(t *testing.T) {
	h := newHandler(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, bigBody())
	})
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	r.Header.Set("Range", "bytes=0-99")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Header().Get("Content-Encoding") != "" {
		t.Fatal("Range requests must not be compressed")
	}
}

func TestStatusCodePreserved(t *testing.T) {
	h := newHandler(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, strings.Repeat(`{"k":"v"}`, 600))
	})
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}
	if rec.Header().Get("Content-Encoding") != "gzip" {
		t.Fatal("compressed 201 expected")
	}
}

func TestFlushSupportsSSE(t *testing.T) {
	h := newHandler(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: one\n\n")
		w.(http.Flusher).Flush()
		_, _ = io.WriteString(w, "data: two\n\n")
		w.(http.Flusher).Flush()
	})
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Header().Get("Content-Encoding") != "gzip" {
		t.Fatal("SSE with gzip client should compress")
	}
	if !rec.Flushed {
		t.Fatal("Flush must reach the underlying writer")
	}
	if got := gunzip(t, rec.Body.Bytes()); got != "data: one\n\ndata: two\n\n" {
		t.Fatalf("SSE payload mangled: %q", got)
	}
}

func TestEmptyBodyNoPanic(t *testing.T) {
	h := newHandler(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusNoContent || rec.Header().Get("Content-Encoding") != "" {
		t.Fatalf("204 mangled: %d %q", rec.Code, rec.Header().Get("Content-Encoding"))
	}
}

func TestInvalidLevelRejected(t *testing.T) {
	if _, err := compress.New(compress.WithConfig(compress.Config{MinSize: 1024, Level: 42})); !errors.Is(err, compress.ErrInvalidConfig) {
		t.Fatalf("level 42 must be rejected, got %v", err)
	}
}
