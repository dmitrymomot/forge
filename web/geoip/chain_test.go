package geoip_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/dmitrymomot/forge/web/geoip"
)

type fakeSource struct {
	loc geoip.Location
	err error
}

func (f fakeSource) Lookup(*http.Request) (geoip.Location, error) { return f.loc, f.err }

func TestChainFirstNonEmptyWins(t *testing.T) {
	miss := fakeSource{}
	hit := fakeSource{loc: geoip.Location{CountryCode: "FR"}}
	second := fakeSource{loc: geoip.Location{CountryCode: "DE"}}
	loc, err := geoip.Chain(miss, hit, second).Lookup(req(nil))
	if err != nil || loc.CountryCode != "FR" {
		t.Fatalf("got (%+v, %v), want FR", loc, err)
	}
}

func TestChainSkipsErroredSource(t *testing.T) {
	boom := fakeSource{err: errors.New("boom")}
	hit := fakeSource{loc: geoip.Location{CountryCode: "US"}}
	loc, err := geoip.Chain(boom, hit).Lookup(req(nil))
	if err != nil || loc.CountryCode != "US" {
		t.Fatalf("got (%+v, %v), want US with nil err", loc, err)
	}
}

func TestChainAllMissOrErrorReturnsFirstError(t *testing.T) {
	boom := fakeSource{err: errors.New("boom")}
	miss := fakeSource{}
	loc, err := geoip.Chain(boom, miss).Lookup(req(nil))
	if !loc.Empty() || err == nil {
		t.Fatalf("got (%+v, %v), want empty + error", loc, err)
	}
}
