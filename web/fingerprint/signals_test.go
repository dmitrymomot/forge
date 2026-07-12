package fingerprint_test

import (
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/web/clientip"
	"github.com/dmitrymomot/forge/web/fingerprint"
)

func signalByName(sigs []fingerprint.Signal, name string) (fingerprint.Signal, bool) {
	for _, s := range sigs {
		if s.Name == name {
			return s, true
		}
	}
	return fingerprint.Signal{}, false
}

func TestDatacenterAndBotSignals(t *testing.T) {
	cfg := fingerprint.Config{Secret: "s", Version: 1, TokenTTL: time.Minute}
	fp, err := fingerprint.New(cfg,
		fingerprint.WithCollectors(fingerprint.Headers(), fingerprint.ClientIP(clientip.RemoteAddrOnly())),
		fingerprint.WithGeoLookup(func(ip netip.Addr) (fingerprint.GeoInfo, bool) {
			return fingerprint.GeoInfo{ASN: 16509, Hosting: true, Continent: "NA"}, true
		}),
		fingerprint.WithUAFamily(func(ua string) (fingerprint.Family, bool) {
			return fingerprint.FamilyBot, true
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "203.0.113.7:5555"
	r.Header.Set("User-Agent", "Googlebot/2.1")
	f, _ := fp.FromRequest(r)
	sigs := fp.Signals(r, f)

	if s, ok := signalByName(sigs, "datacenter-asn"); !ok || !s.Value {
		t.Fatalf("datacenter-asn missing/false: %+v", sigs)
	}
	if s, ok := signalByName(sigs, "bot-ua"); !ok || !s.Value {
		t.Fatalf("bot-ua missing/false: %+v", sigs)
	}
}

func TestUnwiredSeamsEmitNoSignal(t *testing.T) {
	cfg := fingerprint.Config{Secret: "s", Version: 1, TokenTTL: time.Minute}
	fp, err := fingerprint.New(cfg, fingerprint.WithCollectors(fingerprint.Headers()))
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("User-Agent", "curl/8")
	f, _ := fp.FromRequest(r)
	if _, ok := signalByName(fp.Signals(r, f), "datacenter-asn"); ok {
		t.Fatal("datacenter-asn should not emit without geo seam + ip")
	}
}
