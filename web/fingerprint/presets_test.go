package fingerprint_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/web/fingerprint"
)

func TestSessionPreset(t *testing.T) {
	cfg := fingerprint.Config{Secret: "s", Version: 1, TokenTTL: time.Minute}
	fp, err := fingerprint.Session(cfg)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("User-Agent", "Mozilla/5.0")
	f, _ := fp.FromRequest(r)
	if f.Hash == "" {
		t.Fatal("session preset produced no fingerprint")
	}
}

// TestAntifraudPresetBuildsAndCollectsJS proves Antifraud wires the header,
// client-IP, geo, and UA seams, and that the JS collector appended after New
// actually round-trips a probe payload end to end through the preset's
// Fingerprinter (issue token -> ingest -> replay cookie -> collect).
func TestAntifraudPresetBuildsAndCollectsJS(t *testing.T) {
	cfg := fingerprint.Config{Secret: "s", Version: 1, TokenTTL: time.Minute}
	geoSeam := func(_ netip.Addr) (fingerprint.GeoInfo, bool) {
		return fingerprint.GeoInfo{Continent: "NA"}, true
	}
	uaSeam := func(_ string) (fingerprint.Family, bool) {
		return fingerprint.FamilyBrowser, true
	}
	fp, err := fingerprint.Antifraud(cfg, geoSeam, uaSeam, nil)
	if err != nil {
		t.Fatal(err)
	}

	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("User-Agent", "Mozilla/5.0")
	r.RemoteAddr = "203.0.113.7:5555"
	f, err := fp.FromRequest(r)
	if err != nil {
		t.Fatal(err)
	}
	if f.Hash == "" {
		t.Fatal("antifraud preset produced no fingerprint")
	}

	// Issue a token, POST a probe payload to IngestHandler, replay the
	// Set-Cookie onto a fresh request, and confirm the JS collector merges a
	// js-* component — this is the only way to prove fp.JSCollector() was
	// actually appended to fp.cols by the preset.
	tok, err := fp.IssueToken(r)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{
		"token": tok,
		"data":  map[string]any{"timezone": "Asia/Tokyo"},
	})
	ingReq := httptest.NewRequest("POST", "/_fp/ingest", bytes.NewReader(body))
	ingReq.RemoteAddr = "203.0.113.7:5555"
	ingRec := httptest.NewRecorder()
	fp.IngestHandler().ServeHTTP(ingRec, ingReq)
	if ingRec.Code != http.StatusNoContent {
		t.Fatalf("ingest failed: %d", ingRec.Code)
	}

	next := httptest.NewRequest("GET", "/", nil)
	next.RemoteAddr = "203.0.113.7:5555"
	for _, c := range ingRec.Result().Cookies() {
		next.AddCookie(c)
	}
	f2, err := fp.FromRequest(next)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range f2.Components {
		if c.Name == "js-timezone" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected js-timezone component from JSCollector, got %+v", f2.Components)
	}
}
