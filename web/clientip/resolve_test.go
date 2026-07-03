package clientip_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/web/clientip"
)

func req(remote string, headers map[string][]string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = remote
	for k, vs := range headers {
		for _, v := range vs {
			r.Header.Add(k, v)
		}
	}
	return r
}

func TestResolveDefaultIsRemoteAddr(t *testing.T) {
	r := req("203.0.113.9:5555", map[string][]string{
		"X-Forwarded-For": {"1.1.1.1"}, // ignored by the safe default
	})
	assert.Equal(t, "203.0.113.9", clientip.Resolve(r))
}

func TestResolveSingleHeaderStripsPort(t *testing.T) {
	r := req("10.0.0.1:1", map[string][]string{"CF-Connecting-IP": {"198.51.100.7"}})
	assert.Equal(t, "198.51.100.7", clientip.Resolve(r, clientip.SingleHeader("CF-Connecting-IP")))
}

func TestResolveSingleHeaderAbsentFallsBackToRemote(t *testing.T) {
	r := req("192.0.2.5:9", nil)
	assert.Equal(t, "192.0.2.5", clientip.Resolve(r, clientip.SingleHeader("CF-Connecting-IP")))
}

func TestResolveTrustedRangesRightmostUntrusted(t *testing.T) {
	// client -> edge(203.0.113.5) -> our proxy(10.0.0.2) -> app
	r := req("10.0.0.2:80", map[string][]string{
		"X-Forwarded-For": {"203.0.113.5, 10.0.0.2"},
	})
	got := clientip.Resolve(r, clientip.TrustedRanges("10.0.0.0/8"))
	assert.Equal(t, "203.0.113.5", got)
}

func TestResolveTrustedRangesIgnoresForwardedByDefault(t *testing.T) {
	// Forwarded is NOT trusted by default: with only a Forwarded header, resolution
	// falls back to RemoteAddr rather than the (potentially forged) Forwarded value.
	r := req("10.0.0.2:80", map[string][]string{"Forwarded": {`for=203.0.113.9`}})
	assert.Equal(t, "10.0.0.2", clientip.Resolve(r, clientip.TrustedRanges("10.0.0.0/8")))
}

func TestResolveTrustForwardedHeaderOptIn(t *testing.T) {
	// With TrustForwardedHeader, Forwarded for= entries join the chain and are honored.
	r := req("10.0.0.2:80", map[string][]string{"Forwarded": {`for=203.0.113.9`}})
	got := clientip.Resolve(r, clientip.TrustedRanges("10.0.0.0/8"), clientip.TrustForwardedHeader())
	assert.Equal(t, "203.0.113.9", got)
}

func TestResolveTrustedRangesForgedForwardedIgnored(t *testing.T) {
	// Edge manages only XFF; attacker forges Forwarded. Default (XFF-only) returns the
	// real XFF client, never the forged Forwarded value.
	r := req("10.0.0.2:80", map[string][]string{
		"X-Forwarded-For": {"203.0.113.99"},
		"Forwarded":       {"for=6.6.6.6"},
	})
	assert.Equal(t, "203.0.113.99", clientip.Resolve(r, clientip.TrustedRanges("10.0.0.0/8")))
}

func TestResolveMultipleXFFHeaderLinesFlattened(t *testing.T) {
	r := req("10.0.0.2:80", map[string][]string{
		"X-Forwarded-For": {"203.0.113.5", "10.0.0.9"}, // two separate header lines
	})
	got := clientip.Resolve(r, clientip.TrustedRanges("10.0.0.0/8"))
	assert.Equal(t, "203.0.113.5", got)
}

func TestResolveTrustedHopCount(t *testing.T) {
	r := req("10.0.0.2:80", map[string][]string{
		"X-Forwarded-For": {"203.0.113.5, 70.70.70.70"},
	})
	// chain valid = [203.0.113.5, 70.70.70.70, 10.0.0.2]; skip 2 from right -> 203.0.113.5
	assert.Equal(t, "203.0.113.5", clientip.Resolve(r, clientip.TrustedHopCount(2)))
}

func TestResolveLeftmostNonPrivate(t *testing.T) {
	r := req("10.0.0.2:80", map[string][]string{
		"X-Forwarded-For": {"10.9.9.9, 203.0.113.5, 10.0.0.2"},
	})
	assert.Equal(t, "203.0.113.5", clientip.Resolve(r, clientip.LeftmostNonPrivate()))
}

func TestResolveIPv6MappedNormalized(t *testing.T) {
	r := req("[::ffff:192.0.2.7]:9", nil)
	assert.Equal(t, "192.0.2.7", clientip.Resolve(r))
}

func TestResolveMalformedRemoteAddr(t *testing.T) {
	assert.Equal(t, "", clientip.Resolve(req("garbage", nil)))
}

func TestTrustedRangesPanicsOnBadCIDR(t *testing.T) {
	assert.Panics(t, func() { clientip.TrustedRanges("not-a-cidr") })
}

func TestCloudflarePreset(t *testing.T) {
	r := req("10.0.0.1:1", map[string][]string{"CF-Connecting-IP": {"198.51.100.7"}})
	assert.Equal(t, "198.51.100.7", clientip.Resolve(r, clientip.Cloudflare()))
}

func TestEnvoyPreset(t *testing.T) {
	r := req("10.0.0.1:1", map[string][]string{"x-envoy-external-address": {"203.0.113.4"}})
	assert.Equal(t, "203.0.113.4", clientip.Resolve(r, clientip.Envoy()))
}

func TestCloudFrontStripsPort(t *testing.T) {
	r := req("10.0.0.1:1", map[string][]string{"CloudFront-Viewer-Address": {"203.0.113.8:52193"}})
	assert.Equal(t, "203.0.113.8", clientip.Resolve(r, clientip.CloudFront()))
}

func TestTrustPrivateProxies(t *testing.T) {
	r := req("10.1.1.1:80", map[string][]string{"X-Forwarded-For": {"203.0.113.5, 10.1.1.1"}})
	assert.Equal(t, "203.0.113.5", clientip.Resolve(r, clientip.TrustPrivateProxies()))
}

func TestResolveTrustedRangesAllTrustedFallsBackToLeftmostNonPrivate(t *testing.T) {
	// All hops inside a trusted PUBLIC range -> no untrusted hop -> leftmost non-private wins.
	r := req("8.8.8.9:80", map[string][]string{"X-Forwarded-For": {"8.8.8.8, 8.8.8.9"}})
	assert.Equal(t, "8.8.8.8", clientip.Resolve(r, clientip.TrustedRanges("8.8.8.0/24")))
}

func TestResolveTrustedRangesAllTrustedPrivateFallsBackToRemoteAddr(t *testing.T) {
	// All hops trusted AND private -> no non-private hop -> fall back to RemoteAddr (last hop).
	r := req("10.0.0.2:80", map[string][]string{"X-Forwarded-For": {"10.9.9.9"}})
	assert.Equal(t, "10.0.0.2", clientip.Resolve(r, clientip.TrustedRanges("10.0.0.0/8")))
}
