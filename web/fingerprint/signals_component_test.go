package fingerprint_test

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/web/clientip"
	"github.com/dmitrymomot/forge/web/fingerprint"
)

func TestLangMismatchSignal(t *testing.T) {
	cfg := fingerprint.Config{Secret: "s", Version: 1, TokenTTL: time.Minute}
	fp, err := fingerprint.New(cfg, fingerprint.WithCollectors(
		fingerprint.Headers(),
		fingerprint.CollectorFunc(func(_ *http.Request) ([]fingerprint.Component, error) {
			return []fingerprint.Component{{Name: "js-languages", Value: "fr-FR,fr"}}, nil
		}),
	))
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Accept-Language", "en-US,en;q=0.9")
	f, _ := fp.FromRequest(r)
	if s, ok := signalByName(fp.Signals(r, f), "lang-mismatch"); !ok || !s.Value {
		t.Fatalf("expected lang-mismatch=true: %+v", fp.Signals(r, f))
	}
}

func TestGeoTZMismatchSignal(t *testing.T) {
	cfg := fingerprint.Config{Secret: "s", Version: 1, TokenTTL: time.Minute}
	fp, err := fingerprint.New(cfg,
		fingerprint.WithCollectors(
			fingerprint.ClientIP(clientip.RemoteAddrOnly()),
			fingerprint.CollectorFunc(func(_ *http.Request) ([]fingerprint.Component, error) {
				return []fingerprint.Component{{Name: "js-timezone", Value: "Asia/Tokyo"}}, nil
			}),
		),
		fingerprint.WithGeoLookup(func(_ netip.Addr) (fingerprint.GeoInfo, bool) {
			return fingerprint.GeoInfo{Continent: "EU"}, true
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "203.0.113.7:5555"
	f, _ := fp.FromRequest(r)
	if s, ok := signalByName(fp.Signals(r, f), "geo-tz-mismatch"); !ok || !s.Value {
		t.Fatalf("expected geo-tz-mismatch=true: %+v", fp.Signals(r, f))
	}
}

func TestHeadlessSignal(t *testing.T) {
	cfg := fingerprint.Config{Secret: "s", Version: 1, TokenTTL: time.Minute}
	fp, err := fingerprint.New(cfg, fingerprint.WithCollectors(
		fingerprint.CollectorFunc(func(_ *http.Request) ([]fingerprint.Component, error) {
			return []fingerprint.Component{{Name: "js-webdriver", Value: "true"}}, nil
		}),
	))
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/", nil)
	f, _ := fp.FromRequest(r)
	if s, ok := signalByName(fp.Signals(r, f), "headless"); !ok || !s.Value {
		t.Fatalf("expected headless=true")
	}
}
