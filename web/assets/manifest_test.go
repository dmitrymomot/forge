package assets_test

import (
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/dmitrymomot/forge/web/assets"
)

var hashedCSS = regexp.MustCompile(`^/static/app\.[0-9a-f]{8}\.css$`)

func TestURLFingerprintsAtRuntime(t *testing.T) {
	a, err := assets.New(newFS())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	got := a.URL("app.css")
	if !hashedCSS.MatchString(got) {
		t.Fatalf("URL = %q, want /static/app.<8hex>.css", got)
	}
	if s := a.Integrity("app.css"); !strings.HasPrefix(s, "sha384-") {
		t.Fatalf("Integrity = %q, want sha384- prefix", s)
	}
}

func TestURLDeterministicAndContentSensitive(t *testing.T) {
	a1 := mustNew(t, fstest.MapFS{"app.css": {Data: []byte("A")}})
	a2 := mustNew(t, fstest.MapFS{"app.css": {Data: []byte("A")}})
	a3 := mustNew(t, fstest.MapFS{"app.css": {Data: []byte("B")}})
	if a1.URL("app.css") != a2.URL("app.css") {
		t.Fatal("same content produced different hashes")
	}
	if a1.URL("app.css") == a3.URL("app.css") {
		t.Fatal("different content produced same hash")
	}
}

func TestLookupAndUnknownPassthrough(t *testing.T) {
	a := mustNew(t, newFS())
	if _, ok := a.Lookup("app.css"); !ok {
		t.Fatal("Lookup(app.css) not found")
	}
	if _, ok := a.Lookup("missing.css"); ok {
		t.Fatal("Lookup(missing.css) unexpectedly found")
	}
	if got := a.URL("missing.css"); got != "/static/missing.css" {
		t.Fatalf("unknown URL = %q, want /static/missing.css", got)
	}
	if got := a.Integrity("missing.css"); got != "" {
		t.Fatalf("unknown Integrity = %q, want empty", got)
	}
}

func TestDevModePassthrough(t *testing.T) {
	a := mustNew(t, newFS(), assets.WithDev(true))
	if got := a.URL("app.css"); got != "/static/app.css" {
		t.Fatalf("dev URL = %q, want unhashed", got)
	}
	if got := a.Integrity("app.css"); got != "" {
		t.Fatalf("dev Integrity = %q, want empty", got)
	}
}
