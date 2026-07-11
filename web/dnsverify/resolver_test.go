package dnsverify_test

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"testing"

	"github.com/dmitrymomot/forge/web/dnsverify"
)

func TestStaticResolverTXT(t *testing.T) {
	r := dnsverify.NewStaticResolver(
		dnsverify.WithTXT("h.example.com", "a"),
		dnsverify.WithTXT("h.example.com", "b"), // repeatable → appends
	)
	got, err := r.LookupTXT(context.Background(), "h.example.com")
	if err != nil {
		t.Fatalf("LookupTXT: %v", err)
	}
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("LookupTXT = %v, want [a b]", got)
	}
}

func TestStaticResolverNotFound(t *testing.T) {
	r := dnsverify.NewStaticResolver()
	_, err := r.LookupTXT(context.Background(), "missing.example.com")
	var d *net.DNSError
	if !errors.As(err, &d) || !d.IsNotFound {
		t.Fatalf("want IsNotFound DNSError, got %v", err)
	}
}

func TestStaticResolverCNAME(t *testing.T) {
	r := dnsverify.NewStaticResolver(dnsverify.WithCNAME("app.example.com", "ingress.svc.com."))
	got, err := r.LookupCNAME(context.Background(), "app.example.com")
	if err != nil || got != "ingress.svc.com." {
		t.Fatalf("LookupCNAME = %q, %v", got, err)
	}
}

func TestStaticResolverNetIPFamilyFilter(t *testing.T) {
	r := dnsverify.NewStaticResolver(dnsverify.WithIP("example.com",
		netip.MustParseAddr("203.0.113.10"),
		netip.MustParseAddr("2001:db8::1"),
	))
	v4, err := r.LookupNetIP(context.Background(), "ip4", "example.com")
	if err != nil || len(v4) != 1 || !v4[0].Is4() {
		t.Fatalf("ip4 lookup = %v, %v", v4, err)
	}
	v6, err := r.LookupNetIP(context.Background(), "ip6", "example.com")
	if err != nil || len(v6) != 1 || v6[0].Is4() {
		t.Fatalf("ip6 lookup = %v, %v", v6, err)
	}
}

func TestStaticResolverLookupError(t *testing.T) {
	sentinel := errors.New("boom")
	r := dnsverify.NewStaticResolver(
		dnsverify.WithTXT("h.example.com", "a"),
		dnsverify.WithLookupError("h.example.com", sentinel),
	)
	if _, err := r.LookupTXT(context.Background(), "h.example.com"); !errors.Is(err, sentinel) {
		t.Fatalf("want sentinel, got %v", err)
	}
}
