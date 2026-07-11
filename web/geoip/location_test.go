package geoip_test

import (
	"testing"

	"github.com/dmitrymomot/forge/web/geoip"
)

func TestLocationEmpty(t *testing.T) {
	if !(geoip.Location{}).Empty() {
		t.Fatal("zero Location should be Empty")
	}
	for _, l := range []geoip.Location{
		{CountryCode: "US"},
		{ASN: 13335},
		{City: "Berlin"},
	} {
		if l.Empty() {
			t.Fatalf("populated Location %+v should not be Empty", l)
		}
	}
}
