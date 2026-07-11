package assets_test

import (
	"net/http"
	"testing"
	"testing/fstest"

	"github.com/dmitrymomot/forge/web/assets"
)

// precompFS ships a manifest asset plus a .br sibling.
func precompFS() fstest.MapFS {
	return fstest.MapFS{
		"app.a1b2c3d4.css":    {Data: []byte("PLAINCSS")},
		"app.a1b2c3d4.css.br": {Data: []byte("BROTLIBYTES")},
		"manifest.json":       {Data: []byte(`{"app.css":"app.a1b2c3d4.css"}`)},
	}
}

func TestPrecompressedServedWhenAccepted(t *testing.T) {
	a := mustNew(t, precompFS(), assets.WithPrecompressed())
	rec := get(t, a, a.URL("app.css"), http.Header{"Accept-Encoding": {"br"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
	if enc := rec.Header().Get("Content-Encoding"); enc != "br" {
		t.Fatalf("Content-Encoding = %q, want br", enc)
	}
	if ct := rec.Header().Get("Content-Type"); ct == "" {
		t.Fatal("Content-Type should be the original asset's, not empty")
	}
	if v := rec.Header().Get("Vary"); v != "Accept-Encoding" {
		t.Fatalf("Vary = %q, want Accept-Encoding", v)
	}
	if rec.Body.String() != "BROTLIBYTES" {
		t.Fatalf("body = %q, want sibling bytes", rec.Body.String())
	}
}

func TestPrecompressedSkippedWhenNotAccepted(t *testing.T) {
	a := mustNew(t, precompFS(), assets.WithPrecompressed())
	rec := get(t, a, a.URL("app.css"), nil)
	if rec.Header().Get("Content-Encoding") != "" {
		t.Fatal("must not set Content-Encoding without Accept-Encoding")
	}
	if rec.Body.String() != "PLAINCSS" {
		t.Fatalf("body = %q, want plain asset", rec.Body.String())
	}
}

func TestPrecompressedSiblingAbsentFallsThrough(t *testing.T) {
	fsys := fstest.MapFS{
		"only.a1b2c3d4.js": {Data: []byte("NOSIBLING")},
		"manifest.json":    {Data: []byte(`{"only.js":"only.a1b2c3d4.js"}`)},
	}
	a := mustNew(t, fsys, assets.WithPrecompressed())
	rec := get(t, a, a.URL("only.js"), http.Header{"Accept-Encoding": {"br"}})
	if rec.Header().Get("Content-Encoding") != "" {
		t.Fatal("no sibling → must not set Content-Encoding")
	}
	if rec.Body.String() != "NOSIBLING" {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestPrecompressedGzToken(t *testing.T) {
	fsys := fstest.MapFS{
		"a.a1b2c3d4.js":    {Data: []byte("X")},
		"a.a1b2c3d4.js.gz": {Data: []byte("GZ")},
		"manifest.json":    {Data: []byte(`{"a.js":"a.a1b2c3d4.js"}`)},
	}
	a := mustNew(t, fsys, assets.WithPrecompressed())
	rec := get(t, a, a.URL("a.js"), http.Header{"Accept-Encoding": {"gzip"}})
	if enc := rec.Header().Get("Content-Encoding"); enc != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", enc)
	}
}
