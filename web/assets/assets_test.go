package assets_test

import (
	"errors"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/dmitrymomot/forge/web/assets"
)

func newFS() fstest.MapFS {
	return fstest.MapFS{
		"app.css":    {Data: []byte("body{color:red}")},
		"js/app.js":  {Data: []byte("console.log(1)")},
		"index.html": {Data: []byte("<!doctype html><title>x</title>")},
	}
}

// mustNew constructs Assets and fails the test on error, guarding the
// nil-checked result so callers can dereference it safely.
func mustNew(t *testing.T, fsys fs.FS, opts ...assets.Option) *assets.Assets {
	t.Helper()
	a, err := assets.New(fsys, opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return a
}

func TestNewDefaultPrefix(t *testing.T) {
	a, err := assets.New(newFS())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := a.Prefix(); got != "/static/" {
		t.Fatalf("Prefix = %q, want /static/", got)
	}
}

func TestWithPrefixNormalizesTrailingSlash(t *testing.T) {
	a, err := assets.New(newFS(), assets.WithPrefix("/assets"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := a.Prefix(); got != "/assets/" {
		t.Fatalf("Prefix = %q, want /assets/", got)
	}
}

func TestValidateRejectsUnrootedPrefix(t *testing.T) {
	_, err := assets.New(newFS(), assets.WithPrefix("assets"))
	if !errors.Is(err, assets.ErrInvalidConfig) {
		t.Fatalf("err = %v, want ErrInvalidConfig", err)
	}
}
