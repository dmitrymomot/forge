package fingerprint_test

import (
	"fmt"
	"net/http/httptest"
	"net/netip"
	"time"

	"github.com/dmitrymomot/forge/web/fingerprint"
	"github.com/dmitrymomot/forge/web/useragent"
)

// ExampleAntifraud wires the useragent seam for real (imported here in the
// external test package only — the fingerprint core never imports it) and a
// hardcoded geo seam, since web/geoip.Location has no Continent/Hosting
// fields to adapt from.
func ExampleAntifraud() {
	uaSeam := func(ua string) (fingerprint.Family, bool) {
		parsed := useragent.Parse(ua)
		if parsed.IsBot() {
			return fingerprint.FamilyBot, true
		}
		return fingerprint.FamilyBrowser, true
	}
	geoSeam := func(_ netip.Addr) (fingerprint.GeoInfo, bool) {
		return fingerprint.GeoInfo{Continent: "NA", Hosting: false}, true
	}
	cfg := fingerprint.Config{Secret: "example-secret", Version: 1, TokenTTL: time.Minute}
	fp, err := fingerprint.Antifraud(cfg, geoSeam, uaSeam, nil)
	if err != nil {
		panic(err)
	}

	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("User-Agent", "Googlebot/2.1")
	f, _ := fp.FromRequest(r)
	for _, s := range fp.Signals(r, f) {
		if s.Name == "bot-ua" {
			fmt.Println("bot-ua:", s.Value)
		}
	}
	// Output: bot-ua: true
}
