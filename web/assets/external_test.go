package assets_test

import (
	"errors"
	"io/fs"
	"net/http"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/dmitrymomot/forge/web/assets"
)

// externalFS has hashed files physically present plus a flat manifest.json.
func externalFS(manifest string) fstest.MapFS {
	return fstest.MapFS{
		"app.a1b2c3d4.css": {Data: []byte("body{}")},
		"manifest.json":    {Data: []byte(manifest)},
	}
}

func TestExternalStringManifest(t *testing.T) {
	a := mustNew(t, externalFS(`{"app.css":"app.a1b2c3d4.css"}`))
	if got := a.URL("app.css"); got != "/static/app.a1b2c3d4.css" {
		t.Fatalf("URL = %q, want manifest path", got)
	}
	if got := a.Integrity("app.css"); !strings.HasPrefix(got, "sha384-") {
		t.Fatalf("Integrity = %q, want computed sha384-", got)
	}
	rec := get(t, a, a.URL("app.css"), nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("serve code=%d cc=%q", rec.Code, rec.Header().Get("Cache-Control"))
	}
}

func TestExternalObjectManifestKeepsIntegrity(t *testing.T) {
	a := mustNew(t, externalFS(`{"app.css":{"file":"app.a1b2c3d4.css","integrity":"sha384-XYZ"}}`))
	if got := a.Integrity("app.css"); got != "sha384-XYZ" {
		t.Fatalf("Integrity = %q, want sha384-XYZ", got)
	}
}

func TestExternalMalformedIsErrManifest(t *testing.T) {
	_, err := assets.New(externalFS(`{not json`))
	if !errors.Is(err, assets.ErrManifest) {
		t.Fatalf("err = %v, want ErrManifest", err)
	}
}

func TestExternalMissingFileIsErrManifest(t *testing.T) {
	_, err := assets.New(externalFS(`{"app.css":"gone.abcd0000.css"}`))
	if !errors.Is(err, assets.ErrManifest) {
		t.Fatalf("err = %v, want ErrManifest", err)
	}
}

func TestManifestAbsentFallsBackToRuntime(t *testing.T) {
	// No manifest.json present → runtime fingerprinting.
	a := mustNew(t, fstest.MapFS{"app.css": {Data: []byte("x")}})
	if got := a.URL("app.css"); got == "/static/app.css" {
		t.Fatal("expected runtime hashing, got passthrough")
	}
}

type staticReader map[string]assets.Entry

func (s staticReader) Read(fsys fs.FS) (map[string]assets.Entry, error) {
	return map[string]assets.Entry(s), nil
}

func TestWithReaderWins(t *testing.T) {
	fsys := externalFS(`{"app.css":"app.a1b2c3d4.css"}`)
	a := mustNew(t, fsys, assets.WithReader(staticReader{
		"logo": {Path: "app.a1b2c3d4.css"},
	}))
	if got := a.URL("logo"); got != "/static/app.a1b2c3d4.css" {
		t.Fatalf("URL(logo) = %q, want reader entry", got)
	}
	if _, ok := a.Lookup("app.css"); ok {
		t.Fatal("flat manifest should have been ignored when a Reader is set")
	}
}
