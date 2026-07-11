package geoip_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/dmitrymomot/forge/web/geoip"
)

type fakeLocator struct {
	gotIP netip.Addr
	loc   geoip.Location
}

func (f *fakeLocator) Lookup(_ context.Context, ip netip.Addr) (geoip.Location, error) {
	f.gotIP = ip
	return f.loc, nil
}

func TestFromLocatorResolvesRemoteAddr(t *testing.T) {
	fl := &fakeLocator{loc: geoip.Location{CountryCode: "JP"}}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "203.0.113.7:5555"

	loc, err := geoip.FromLocator(fl).Lookup(r)
	if err != nil {
		t.Fatal(err)
	}
	if fl.gotIP.String() != "203.0.113.7" {
		t.Fatalf("locator saw ip %q, want 203.0.113.7", fl.gotIP)
	}
	if loc.CountryCode != "JP" {
		t.Fatalf("country = %q, want JP", loc.CountryCode)
	}
}

func TestFromLocatorUnparseableIPIsMiss(t *testing.T) {
	fl := &fakeLocator{loc: geoip.Location{CountryCode: "JP"}}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "not-an-ip"

	loc, err := geoip.FromLocator(fl).Lookup(r)
	if err != nil || !loc.Empty() {
		t.Fatalf("got (%+v, %v), want empty + nil", loc, err)
	}
}
