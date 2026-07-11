package assets_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmitrymomot/forge/web/assets"
)

func contains(s, sub string) bool { return strings.Contains(s, sub) }

func httptestRequest(method, target string) *http.Request {
	return httptest.NewRequest(method, target, nil)
}

func recorderFor(a *assets.Assets, r *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	a.ServeHTTP(rec, r)
	return rec
}

func TestSPAServesIndexForNavigation(t *testing.T) {
	a := mustNew(t, newFS(), assets.WithSPA("index.html"))
	rec := get(t, a, "/static/dashboard/settings", nil) // no extension
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Fatalf("Cache-Control = %q, want no-cache (never immutable)", cc)
	}
	if !contains(rec.Body.String(), "<title>x</title>") {
		t.Fatalf("body = %q, want index.html", rec.Body.String())
	}
}

func TestSPADoesNotHideMissingAsset(t *testing.T) {
	a := mustNew(t, newFS(), assets.WithSPA("index.html"))
	rec := get(t, a, "/static/typo.js", http.Header{"Accept": {"*/*"}})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing asset code = %d, want 404", rec.Code)
	}
}

func TestSPAAcceptHTMLWithExtension(t *testing.T) {
	a := mustNew(t, newFS(), assets.WithSPA("index.html"))
	rec := get(t, a, "/static/some.thing", http.Header{"Accept": {"text/html"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200 (Accept text/html falls back)", rec.Code)
	}
}

func TestSPAIgnoresNonGET(t *testing.T) {
	a := mustNew(t, newFS(), assets.WithSPA("index.html"))
	r := httptestRequest(http.MethodPost, "/static/dashboard")
	rec := recorderFor(a, r)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("POST code = %d, want 404", rec.Code)
	}
}

func TestSPAOffByDefault(t *testing.T) {
	a := mustNew(t, newFS())
	rec := get(t, a, "/static/dashboard", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("no WithSPA → code = %d, want 404", rec.Code)
	}
}

func TestSPACustomPredicate(t *testing.T) {
	never := func(*http.Request) bool { return false }
	a := mustNew(t, newFS(), assets.WithSPA("index.html", never))
	rec := get(t, a, "/static/dashboard", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("custom never-predicate code = %d, want 404", rec.Code)
	}
}
