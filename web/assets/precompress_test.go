package assets_test

import (
	"mime"
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
	if v := rec.Header().Get("Vary"); v != "Accept-Encoding" {
		t.Fatalf("Vary = %q, want Accept-Encoding", v)
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
	if v := rec.Header().Get("Vary"); v != "Accept-Encoding" {
		t.Fatalf("Vary = %q, want Accept-Encoding", v)
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

// TestPrecompressedDistinctEtagAnd304 verifies the compressed representation
// gets its own strong Etag (RFC 9110 §8.8.3), distinct from the identity
// Etag, and that a matching If-None-Match on the encoded request 304s.
func TestPrecompressedDistinctEtagAnd304(t *testing.T) {
	a := mustNew(t, precompFS(), assets.WithPrecompressed())
	url := a.URL("app.css")

	identity := get(t, a, url, nil)
	identityEtag := identity.Header().Get("Etag")
	if identityEtag == "" {
		t.Fatal("identity Etag must not be empty")
	}

	compressed := get(t, a, url, http.Header{"Accept-Encoding": {"br"}})
	compressedEtag := compressed.Header().Get("Etag")
	if compressedEtag == "" {
		t.Fatal("compressed Etag must not be empty")
	}
	if compressedEtag == identityEtag {
		t.Fatalf("compressed Etag %q must differ from identity Etag %q", compressedEtag, identityEtag)
	}

	rec := get(t, a, url, http.Header{
		"Accept-Encoding": {"br"},
		"If-None-Match":   {compressedEtag},
	})
	if rec.Code != http.StatusNotModified {
		t.Fatalf("code = %d, want 304", rec.Code)
	}
}

// TestPrecompressedQZeroRefused verifies that "br;q=0" is an explicit client
// refusal of brotli (RFC 9110 §12.5.3), not a substring match, so the
// identity bytes are served instead of the compressed sibling.
func TestPrecompressedQZeroRefused(t *testing.T) {
	a := mustNew(t, precompFS(), assets.WithPrecompressed())
	rec := get(t, a, a.URL("app.css"), http.Header{"Accept-Encoding": {"br;q=0, gzip"}})
	if enc := rec.Header().Get("Content-Encoding"); enc != "" {
		t.Fatalf("Content-Encoding = %q, want empty (br refused, no gzip sibling)", enc)
	}
	if rec.Body.String() != "PLAINCSS" {
		t.Fatalf("body = %q, want identity bytes", rec.Body.String())
	}
}

// TestPrecompressedTokenNoSubstringMatch verifies that "brotli" in
// Accept-Encoding does not false-match the "br" content-coding token.
func TestPrecompressedTokenNoSubstringMatch(t *testing.T) {
	a := mustNew(t, precompFS(), assets.WithPrecompressed())
	rec := get(t, a, a.URL("app.css"), http.Header{"Accept-Encoding": {"brotli"}})
	if enc := rec.Header().Get("Content-Encoding"); enc != "" {
		t.Fatalf("Content-Encoding = %q, want empty (brotli != br)", enc)
	}
	if rec.Body.String() != "PLAINCSS" {
		t.Fatalf("body = %q, want identity bytes", rec.Body.String())
	}
}

// TestPrecompressedIfNoneMatchWildcard verifies If-None-Match: * 304s a
// precompressed asset (RFC 9110 §13.1.2).
func TestPrecompressedIfNoneMatchWildcard(t *testing.T) {
	a := mustNew(t, precompFS(), assets.WithPrecompressed())
	rec := get(t, a, a.URL("app.css"), http.Header{
		"Accept-Encoding": {"br"},
		"If-None-Match":   {"*"},
	})
	if rec.Code != http.StatusNotModified {
		t.Fatalf("code = %d, want 304", rec.Code)
	}
}

// TestPrecompressedIfNoneMatchList verifies a comma-separated If-None-Match
// list 304s when it contains the compressed representation's Etag.
func TestPrecompressedIfNoneMatchList(t *testing.T) {
	a := mustNew(t, precompFS(), assets.WithPrecompressed())
	url := a.URL("app.css")
	compressedEtag := get(t, a, url, http.Header{"Accept-Encoding": {"br"}}).Header().Get("Etag")

	rec := get(t, a, url, http.Header{
		"Accept-Encoding": {"br"},
		"If-None-Match":   {`"unrelated-etag", ` + compressedEtag + `, "another"`},
	})
	if rec.Code != http.StatusNotModified {
		t.Fatalf("code = %d, want 304", rec.Code)
	}
}

// TestPrecompressedWeakIfNoneMatch verifies a weak ("W/"-prefixed) validator
// still matches the compressed representation's strong Etag, since
// etagMatches strips the weak prefix before comparing.
func TestPrecompressedWeakIfNoneMatch(t *testing.T) {
	a := mustNew(t, precompFS(), assets.WithPrecompressed())
	url := a.URL("app.css")
	compressedEtag := get(t, a, url, http.Header{"Accept-Encoding": {"br"}}).Header().Get("Etag")

	rec := get(t, a, url, http.Header{
		"Accept-Encoding": {"br"},
		"If-None-Match":   {"W/" + compressedEtag},
	})
	if rec.Code != http.StatusNotModified {
		t.Fatalf("code = %d, want 304", rec.Code)
	}
}

// TestPrecompressedUnknownContentTypeFallsThrough verifies that when the
// fingerprinted asset's Content-Type can't be determined, the compressed
// sibling is not served (net/http would sniff the compressed stream and
// mislabel it) and the identity bytes are served instead.
func TestPrecompressedUnknownContentTypeFallsThrough(t *testing.T) {
	if ct := mime.TypeByExtension(".dat"); ct != "" {
		t.Skipf("platform resolves .dat to %q, need an extension with no known type", ct)
	}
	fsys := fstest.MapFS{
		"data.a1b2c3d4.dat":    {Data: []byte("PLAINDATA")},
		"data.a1b2c3d4.dat.br": {Data: []byte("BROTLIDATA")},
		"manifest.json":        {Data: []byte(`{"data.dat":"data.a1b2c3d4.dat"}`)},
	}
	a := mustNew(t, fsys, assets.WithPrecompressed())
	rec := get(t, a, a.URL("data.dat"), http.Header{"Accept-Encoding": {"br"}})
	if enc := rec.Header().Get("Content-Encoding"); enc != "" {
		t.Fatalf("Content-Encoding = %q, want empty (identity fallback)", enc)
	}
	if rec.Body.String() != "PLAINDATA" {
		t.Fatalf("body = %q, want identity bytes", rec.Body.String())
	}
}
