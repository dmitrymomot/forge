package fingerprint_test

import (
	"net/http/httptest"
	"testing"

	"github.com/dmitrymomot/forge/web/fingerprint"
)

func TestClientHintsCollector(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Sec-CH-UA", `"Chromium";v="126", "Not.A/Brand";v="24"`)
	r.Header.Set("Sec-CH-UA-Platform", `"Windows"`)
	r.Header.Set("Sec-CH-UA-Mobile", "?0")
	r.Header.Set("Device-Memory", "8")
	// Sec-CH-UA-Arch, -Bitness, -Model, DPR absent.
	comps, err := fingerprint.ClientHints().Collect(r)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, c := range comps {
		got[c.Name] = c.Value
	}
	if got["ch-ua-platform"] != `"Windows"` || got["ch-ua-mobile"] != "?0" || got["device-memory"] != "8" {
		t.Fatalf("unexpected components: %v", got)
	}
	if _, ok := got["ch-ua-arch"]; ok {
		t.Fatalf("absent hint must not emit a component: %v", got)
	}
}
