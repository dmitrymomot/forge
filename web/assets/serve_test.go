package assets_test

import (
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmitrymomot/forge/web/assets"
)

func get(t *testing.T, a *assets.Assets, target string, hdr http.Header) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, target, nil)
	maps.Copy(r.Header, hdr)
	rec := httptest.NewRecorder()
	a.ServeHTTP(rec, r)
	return rec
}

func TestServeFingerprintedImmutable(t *testing.T) {
	a := mustNew(t, newFS())
	url := a.URL("app.css")
	rec := get(t, a, url, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Fatalf("Cache-Control = %q, want immutable", cc)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/css") {
		t.Fatalf("Content-Type = %q, want text/css", ct)
	}
	if rec.Header().Get("Etag") == "" {
		t.Fatal("missing Etag")
	}
	if body := rec.Body.String(); body != "body{color:red}" {
		t.Fatalf("body = %q", body)
	}
}

func TestServeIfNoneMatch304(t *testing.T) {
	a := mustNew(t, newFS())
	url := a.URL("app.css")
	etag := get(t, a, url, nil).Header().Get("Etag")
	rec := get(t, a, url, http.Header{"If-None-Match": {etag}})
	if rec.Code != http.StatusNotModified {
		t.Fatalf("code = %d, want 304", rec.Code)
	}
}

func TestServeRange(t *testing.T) {
	a := mustNew(t, newFS())
	url := a.URL("app.css")
	rec := get(t, a, url, http.Header{"Range": {"bytes=0-3"}})
	if rec.Code != http.StatusPartialContent {
		t.Fatalf("code = %d, want 206", rec.Code)
	}
	if rec.Body.String() != "body" {
		t.Fatalf("range body = %q, want body", rec.Body.String())
	}
}

func TestServePlainNoCache(t *testing.T) {
	a := mustNew(t, newFS())
	rec := get(t, a, "/static/app.css", nil) // unhashed path → plain branch
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Fatalf("Cache-Control = %q, want no-cache", cc)
	}
	if rec.Header().Get("Etag") == "" {
		t.Fatal("plain response missing Etag")
	}
}

func TestServeMissing404AndTraversal(t *testing.T) {
	a := mustNew(t, newFS())
	if rec := get(t, a, "/static/nope.js", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("missing code = %d, want 404", rec.Code)
	}
	if rec := get(t, a, "/static/../secret", nil); rec.Code == http.StatusOK {
		t.Fatal("traversal served a 200")
	}
	if rec := get(t, a, "/elsewhere/x", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("off-prefix code = %d, want 404", rec.Code)
	}
}

func TestServeDevReadsPlain(t *testing.T) {
	a := mustNew(t, newFS(), assets.WithDev(true))
	rec := get(t, a, "/static/app.css", nil)
	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("dev serve code=%d cc=%q", rec.Code, rec.Header().Get("Cache-Control"))
	}
}

func TestWithCacheControlOverride(t *testing.T) {
	a := mustNew(t, newFS(), assets.WithCacheControl("public, max-age=60", "private"))
	rec := get(t, a, a.URL("app.css"), nil)
	if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=60" {
		t.Fatalf("Cache-Control = %q, want override", cc)
	}
}
