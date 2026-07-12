package fingerprint_test

import (
	"net/http/httptest"
	"testing"

	"github.com/dmitrymomot/forge/web/fingerprint"
)

func TestHeadersCollector(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("User-Agent", "Mozilla/5.0")
	r.Header.Set("Accept-Language", "en-US,en;q=0.9")
	// Accept and Accept-Encoding absent.
	comps, err := fingerprint.Headers().Collect(r)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, c := range comps {
		got[c.Name] = c.Value
	}
	if got["ua"] != "Mozilla/5.0" || got["accept-language"] != "en-US,en;q=0.9" {
		t.Fatalf("unexpected components: %v", got)
	}
	if _, ok := got["accept"]; ok {
		t.Fatalf("absent header must not emit a component: %v", got)
	}
}
