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

func TestTLSUAMismatchSignal(t *testing.T) {
	cfg := fingerprint.Config{Secret: "s", Version: 1, TokenTTL: time.Minute}
	fp, err := fingerprint.New(cfg,
		fingerprint.WithCollectors(
			fingerprint.CollectorFunc(func(_ *http.Request) ([]fingerprint.Component, error) {
				return []fingerprint.Component{
					{Name: "tls", Value: "t13d1516h2_8daaf6152771_02713d6af862"},
					{Name: "ua", Value: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"},
				}, nil
			}),
		),
		fingerprint.WithUAFamily(func(_ string) (fingerprint.Family, bool) {
			return fingerprint.FamilyBrowser, true
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/", nil)
	f, _ := fp.FromRequest(r)
	sigs := fp.Signals(r, f)
	s, ok := signalByName(sigs, "tls-ua-mismatch")
	if !ok {
		t.Fatalf("expected tls-ua-mismatch to be emitted: %+v", sigs)
	}
	if s.Value {
		t.Fatalf("expected tls-ua-mismatch=false (automationJA4 ships empty): %+v", s)
	}
}

func TestTLSUAMismatchNotEmittedWithoutUASeam(t *testing.T) {
	cfg := fingerprint.Config{Secret: "s", Version: 1, TokenTTL: time.Minute}
	fp, err := fingerprint.New(cfg,
		fingerprint.WithCollectors(
			fingerprint.CollectorFunc(func(_ *http.Request) ([]fingerprint.Component, error) {
				return []fingerprint.Component{
					{Name: "tls", Value: "t13d1516h2_8daaf6152771_02713d6af862"},
					{Name: "ua", Value: "Mozilla/5.0"},
				}, nil
			}),
		),
	)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/", nil)
	f, _ := fp.FromRequest(r)
	if _, ok := signalByName(fp.Signals(r, f), "tls-ua-mismatch"); ok {
		t.Fatal("tls-ua-mismatch should not emit without a wired UA seam")
	}
}

func TestTLSUAMismatchNotEmittedWithoutTLSComponent(t *testing.T) {
	cfg := fingerprint.Config{Secret: "s", Version: 1, TokenTTL: time.Minute}
	fp, err := fingerprint.New(cfg,
		fingerprint.WithCollectors(fingerprint.Headers()),
		fingerprint.WithUAFamily(func(_ string) (fingerprint.Family, bool) {
			return fingerprint.FamilyBrowser, true
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("User-Agent", "Mozilla/5.0")
	f, _ := fp.FromRequest(r)
	if _, ok := signalByName(fp.Signals(r, f), "tls-ua-mismatch"); ok {
		t.Fatal("tls-ua-mismatch should not emit without a tls component")
	}
}

func TestHeaderAnomalySignal(t *testing.T) {
	cfg := fingerprint.Config{Secret: "s", Version: 1, TokenTTL: time.Minute}
	fp, err := fingerprint.New(cfg,
		fingerprint.WithCollectors(fingerprint.Headers()),
		fingerprint.WithUAFamily(func(_ string) (fingerprint.Family, bool) {
			return fingerprint.FamilyBrowser, true
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	chromeUA := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"

	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("User-Agent", chromeUA)
	f, _ := fp.FromRequest(r)
	if s, ok := signalByName(fp.Signals(r, f), "header-anomaly"); !ok || !s.Value {
		t.Fatalf("expected header-anomaly=true when Sec-Ch-Ua/Sec-Fetch-Site are absent: %+v", fp.Signals(r, f))
	}

	r2 := httptest.NewRequest("GET", "/", nil)
	r2.Header.Set("User-Agent", chromeUA)
	r2.Header.Set("Sec-Ch-Ua", `"Chromium";v="126"`)
	r2.Header.Set("Sec-Fetch-Site", "same-origin")
	f2, _ := fp.FromRequest(r2)
	if s, ok := signalByName(fp.Signals(r2, f2), "header-anomaly"); !ok || s.Value {
		t.Fatalf("expected header-anomaly=false when Sec-Ch-Ua/Sec-Fetch-Site are present: %+v", fp.Signals(r2, f2))
	}
}

func TestHeaderAnomalyNotEmittedForNonChromeUA(t *testing.T) {
	cfg := fingerprint.Config{Secret: "s", Version: 1, TokenTTL: time.Minute}
	fp, err := fingerprint.New(cfg,
		fingerprint.WithCollectors(fingerprint.Headers()),
		fingerprint.WithUAFamily(func(_ string) (fingerprint.Family, bool) {
			return fingerprint.FamilyBrowser, true
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15")
	f, _ := fp.FromRequest(r)
	if _, ok := signalByName(fp.Signals(r, f), "header-anomaly"); ok {
		t.Fatal("header-anomaly should not emit for a non-Chrome browser UA")
	}
}

func TestCHUAMismatchSignal(t *testing.T) {
	newFP := func(chPlatform, jsPlatform string) *fingerprint.Fingerprinter {
		cfg := fingerprint.Config{Secret: "s", Version: 1, TokenTTL: time.Minute}
		fp, err := fingerprint.New(cfg, fingerprint.WithCollectors(
			fingerprint.CollectorFunc(func(_ *http.Request) ([]fingerprint.Component, error) {
				return []fingerprint.Component{
					{Name: "ch-ua-platform", Value: chPlatform},
					{Name: "js-platform", Value: jsPlatform},
				}, nil
			}),
		))
		if err != nil {
			t.Fatal(err)
		}
		return fp
	}

	// Contradiction: CH says Windows, JS says a Mac.
	fp := newFP(`"Windows"`, "MacIntel")
	r := httptest.NewRequest("GET", "/", nil)
	f, _ := fp.FromRequest(r)
	if s, ok := signalByName(fp.Signals(r, f), "ch-ua-mismatch"); !ok || !s.Value {
		t.Fatalf("expected ch-ua-mismatch=true: %+v", fp.Signals(r, f))
	}

	// Agreement: CH Windows, JS Win32.
	fp = newFP(`"Windows"`, "Win32")
	r = httptest.NewRequest("GET", "/", nil)
	f, _ = fp.FromRequest(r)
	if s, ok := signalByName(fp.Signals(r, f), "ch-ua-mismatch"); !ok || s.Value {
		t.Fatalf("expected ch-ua-mismatch=false: %+v", fp.Signals(r, f))
	}

	// Ambiguous JS platform (Android/desktop Linux share "Linux armv8l") → not emitted.
	fp = newFP(`"Android"`, "Linux armv8l")
	r = httptest.NewRequest("GET", "/", nil)
	f, _ = fp.FromRequest(r)
	if _, ok := signalByName(fp.Signals(r, f), "ch-ua-mismatch"); ok {
		t.Fatal("ch-ua-mismatch must not emit on an ambiguous js-platform")
	}
}

func TestFetchMetadataAnomalySignal(t *testing.T) {
	cfg := fingerprint.Config{Secret: "s", Version: 1, TokenTTL: time.Minute}
	fp, err := fingerprint.New(cfg, fingerprint.WithCollectors(fingerprint.Headers()))
	if err != nil {
		t.Fatal(err)
	}

	// Contradiction a real browser never sends: navigate mode with empty dest.
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Sec-Fetch-Mode", "navigate")
	r.Header.Set("Sec-Fetch-Dest", "empty")
	f, _ := fp.FromRequest(r)
	if s, ok := signalByName(fp.Signals(r, f), "fetch-metadata-anomaly"); !ok || !s.Value {
		t.Fatalf("expected fetch-metadata-anomaly=true: %+v", fp.Signals(r, f))
	}

	// Normal top-level navigation.
	r2 := httptest.NewRequest("GET", "/", nil)
	r2.Header.Set("Sec-Fetch-Site", "none")
	r2.Header.Set("Sec-Fetch-Mode", "navigate")
	r2.Header.Set("Sec-Fetch-Dest", "document")
	f2, _ := fp.FromRequest(r2)
	if s, ok := signalByName(fp.Signals(r2, f2), "fetch-metadata-anomaly"); !ok || s.Value {
		t.Fatalf("expected fetch-metadata-anomaly=false: %+v", fp.Signals(r2, f2))
	}

	// No Sec-Fetch-* headers at all → not emitted.
	r3 := httptest.NewRequest("GET", "/", nil)
	f3, _ := fp.FromRequest(r3)
	if _, ok := signalByName(fp.Signals(r3, f3), "fetch-metadata-anomaly"); ok {
		t.Fatal("fetch-metadata-anomaly must not emit without any Sec-Fetch-* header")
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
