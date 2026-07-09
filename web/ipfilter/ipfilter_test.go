package ipfilter_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmitrymomot/forge/web/clientip"
	"github.com/dmitrymomot/forge/web/ipfilter"
	"github.com/dmitrymomot/forge/web/middleware"
)

func serve(t *testing.T, mw middleware.Middleware, r *http.Request) int {
	t.Helper()
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec.Code
}

func reqFrom(remote string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = remote
	return r
}

func TestAllowlistGate(t *testing.T) {
	mw := ipfilter.New(ipfilter.WithAllow("203.0.113.0/24", "198.51.100.7"))
	if c := serve(t, mw, reqFrom("203.0.113.42:9")); c != http.StatusOK {
		t.Fatalf("in-range: %d", c)
	}
	if c := serve(t, mw, reqFrom("198.51.100.7:9")); c != http.StatusOK {
		t.Fatalf("bare IP: %d", c)
	}
	if c := serve(t, mw, reqFrom("8.8.8.8:9")); c != http.StatusForbidden {
		t.Fatalf("outsider: %d, want 403", c)
	}
}

func TestDenylistOnly(t *testing.T) {
	mw := ipfilter.New(ipfilter.WithDeny("192.0.2.0/24"))
	if c := serve(t, mw, reqFrom("192.0.2.15:9")); c != http.StatusForbidden {
		t.Fatalf("denied: %d, want 403", c)
	}
	if c := serve(t, mw, reqFrom("8.8.8.8:9")); c != http.StatusOK {
		t.Fatalf("not denied: %d", c)
	}
}

func TestDenyWinsOverAllow(t *testing.T) {
	mw := ipfilter.New(
		ipfilter.WithAllow("203.0.113.0/24"),
		ipfilter.WithDeny("203.0.113.66"),
	)
	if c := serve(t, mw, reqFrom("203.0.113.10:9")); c != http.StatusOK {
		t.Fatalf("allowed in range: %d", c)
	}
	if c := serve(t, mw, reqFrom("203.0.113.66:9")); c != http.StatusForbidden {
		t.Fatalf("deny should win: %d, want 403", c)
	}
	if c := serve(t, mw, reqFrom("8.8.8.8:9")); c != http.StatusForbidden {
		t.Fatalf("gate default-deny: %d, want 403", c)
	}
}

func TestIPv6Deny(t *testing.T) {
	mw := ipfilter.New(ipfilter.WithDeny("2001:db8::/32"))
	if c := serve(t, mw, reqFrom("[2001:db8::1]:9")); c != http.StatusForbidden {
		t.Fatalf("ipv6 deny: %d, want 403", c)
	}
}

func TestUnresolvableUnderAllowlistBlocked(t *testing.T) {
	mw := ipfilter.New(ipfilter.WithAllow("203.0.113.0/24"))
	r := reqFrom("garbage-not-an-ip")
	if c := serve(t, mw, r); c != http.StatusForbidden {
		t.Fatalf("unresolvable under allowlist: %d, want 403", c)
	}
}

func TestClientIPProxyTrust(t *testing.T) {
	mw := ipfilter.New(
		ipfilter.WithAllow("203.0.113.0/24"),
		ipfilter.WithClientIP(clientip.XRealIP()),
	)
	r := reqFrom("10.0.0.1:9")
	r.Header.Set("X-Real-IP", "203.0.113.9")
	if c := serve(t, mw, r); c != http.StatusOK {
		t.Fatalf("trusted header should allow: %d", c)
	}

	// Without WithClientIP the header is ignored (RemoteAddr only) => blocked.
	mwDefault := ipfilter.New(ipfilter.WithAllow("203.0.113.0/24"))
	r2 := reqFrom("10.0.0.1:9")
	r2.Header.Set("X-Real-IP", "203.0.113.9")
	if c := serve(t, mwDefault, r2); c != http.StatusForbidden {
		t.Fatalf("untrusted header must be ignored: %d, want 403", c)
	}
}

func TestResponderOverride(t *testing.T) {
	mw := ipfilter.New(
		ipfilter.WithDeny("0.0.0.0/0"),
		ipfilter.WithResponder(func(w http.ResponseWriter, _ *http.Request, _ error) {
			w.WriteHeader(http.StatusTeapot)
		}),
	)
	if c := serve(t, mw, reqFrom("1.2.3.4:9")); c != http.StatusTeapot {
		t.Fatalf("custom responder: %d, want 418", c)
	}
}

func benchServe(b *testing.B, mw middleware.Middleware, r *http.Request) {
	b.Helper()
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	b.ReportAllocs()
	for range b.N {
		h.ServeHTTP(rec, r)
	}
}

func BenchmarkServeAllowed(b *testing.B) {
	mw := ipfilter.New(ipfilter.WithAllow("203.0.113.0/24"))
	benchServe(b, mw, reqFrom("203.0.113.9:9"))
}

func BenchmarkServeBlocked(b *testing.B) {
	mw := ipfilter.New(ipfilter.WithAllow("203.0.113.0/24"))
	benchServe(b, mw, reqFrom("8.8.8.8:9"))
}

func BenchmarkServeLargeList(b *testing.B) {
	cidrs := make([]string, 0, 100)
	for i := range 100 {
		cidrs = append(cidrs, fmt.Sprintf("10.%d.0.0/16", i))
	}
	mw := ipfilter.New(ipfilter.WithAllow(cidrs...))
	benchServe(b, mw, reqFrom("10.99.0.1:9")) // matches the last entry (worst case)
}
