package fingerprint_test

import (
	"net/http/httptest"
	"testing"

	"github.com/dmitrymomot/forge/web/clientip"
	"github.com/dmitrymomot/forge/web/fingerprint"
)

func TestClientIPCollector(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "203.0.113.7:44321"
	comps, err := fingerprint.ClientIP(clientip.RemoteAddrOnly()).Collect(r)
	if err != nil {
		t.Fatal(err)
	}
	if len(comps) != 1 || comps[0].Name != "ip" || comps[0].Value != "203.0.113.7" {
		t.Fatalf("unexpected: %+v", comps)
	}
}
