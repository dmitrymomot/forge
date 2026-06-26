package request_test

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/request"
)

func TestClientIPPriority(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.1:1234"
	r.Header.Set("X-Real-IP", "203.0.113.5")
	r.Header.Set("CF-Connecting-IP", "198.51.100.7")
	assert.Equal(t, "198.51.100.7", request.ClientIP(r)) // CF outranks X-Real-IP
}

func TestClientIPFallbackRemoteAddr(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "192.0.2.9:5555"
	assert.Equal(t, "192.0.2.9", request.ClientIP(r))
}

func TestClientIPXFFFirstValid(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.1:1"
	r.Header.Set("X-Forwarded-For", "garbage, 203.0.113.5, 70.41.3.18")
	assert.Equal(t, "203.0.113.5", request.ClientIP(r))
}

func TestClientIPForwarded(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.1:1"
	r.Header.Set("Forwarded", `for=192.0.2.60;proto=http, for=198.51.100.17`)
	assert.Equal(t, "192.0.2.60", request.ClientIP(r))
}

func TestClientIPPinnedHeader(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.1:1"
	r.Header.Set("X-Forwarded-For", "1.2.3.4")
	r.Header.Set("CF-Connecting-IP", "9.9.9.9")
	// pin to a header that is absent -> ignore XFF/CF, fall back to RemoteAddr
	assert.Equal(t, "10.0.0.1", request.ClientIP(r, request.WithClientIPHeaders("X-Real-IP")))
}

func TestClientIPTrustedProxies(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.1:1" // trusted hop
	r.Header.Set("X-Forwarded-For", "203.0.113.5, 10.0.0.2")
	trusted := netip.MustParsePrefix("10.0.0.0/8")
	assert.Equal(t, "203.0.113.5", request.ClientIP(r, request.WithTrustedProxies(trusted)))
}

func TestClientIPMalformedRemoteAddr(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "not-an-ip" // no usable headers, unparseable peer
	assert.Equal(t, "", request.ClientIP(r))
}

func TestClientIPAllTrusted(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.1:1"
	r.Header.Set("X-Forwarded-For", "10.1.1.1, 10.2.2.2")
	trusted := netip.MustParsePrefix("10.0.0.0/8")
	// every hop is trusted -> fall back to the left-most (original client)
	assert.Equal(t, "10.1.1.1", request.ClientIP(r, request.WithTrustedProxies(trusted)))
}

func TestClientIPTrustedIgnoresForwarded(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "203.0.113.9:443"
	r.Header.Set("Forwarded", "for=198.51.100.1") // not consulted in trusted mode
	trusted := netip.MustParsePrefix("10.0.0.0/8")
	// no XFF; chain is just RemoteAddr (untrusted) -> returned, Forwarded ignored
	assert.Equal(t, "203.0.113.9", request.ClientIP(r, request.WithTrustedProxies(trusted)))
}
