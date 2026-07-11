package dnsverify_test

import (
	"context"
	"net/netip"
	"strings"
	"testing"

	"github.com/dmitrymomot/forge/web/dnsverify"
)

func TestTXTChallengeShapeAndUniqueness(t *testing.T) {
	v, err := dnsverify.New(dnsverify.WithResolver(dnsverify.NewStaticResolver()))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c1 := v.TXTChallenge("example.com")
	c2 := v.TXTChallenge("example.com")

	if c1.Record != dnsverify.TXT {
		t.Errorf("Record = %v, want TXT", c1.Record)
	}
	if c1.Host != "_forge-verify.example.com" {
		t.Errorf("Host = %q", c1.Host)
	}
	if len(c1.Expect) != 1 || !strings.HasPrefix(c1.Expect[0], "forge-verification=") {
		t.Errorf("Expect = %v, want one forge-verification= value", c1.Expect)
	}
	if c1.Expect[0] == c2.Expect[0] {
		t.Errorf("tokens must be unique across calls: %q", c1.Expect[0])
	}
}

func TestTXTChallengeHonorsConfig(t *testing.T) {
	v, err := dnsverify.New(
		dnsverify.WithResolver(dnsverify.NewStaticResolver()),
		dnsverify.WithLabel("_custom"),
		dnsverify.WithTokenBytes(32),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c := v.TXTChallenge("acme.test")
	if c.Host != "_custom.acme.test" {
		t.Errorf("Host = %q, want _custom.acme.test", c.Host)
	}
	// 32 raw bytes → 43 unpadded base64url chars after the prefix.
	token := strings.TrimPrefix(c.Expect[0], "forge-verification=")
	if len(token) != 43 {
		t.Errorf("token length = %d, want 43 for 32 bytes", len(token))
	}
}

func TestRoutingConstructors(t *testing.T) {
	v, err := dnsverify.New(dnsverify.WithResolver(dnsverify.NewStaticResolver()))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	cn := v.CNAMEChallenge("app.example.com", "ingress.svc.com")
	if cn.Record != dnsverify.CNAME || cn.Host != "app.example.com" || cn.Expect[0] != "ingress.svc.com" {
		t.Errorf("CNAMEChallenge = %+v", cn)
	}

	a := v.AChallenge("example.com", netip.MustParseAddr("203.0.113.10"))
	if a.Record != dnsverify.A || a.Expect[0] != "203.0.113.10" {
		t.Errorf("AChallenge = %+v", a)
	}

	aaaa := v.AAAAChallenge("example.com", netip.MustParseAddr("2001:db8::1"))
	if aaaa.Record != dnsverify.AAAA || aaaa.Expect[0] != "2001:db8::1" {
		t.Errorf("AAAAChallenge = %+v", aaaa)
	}
}

func TestTXTChallengeRoundTripsThroughVerify(t *testing.T) {
	v, err := dnsverify.New(dnsverify.WithResolver(dnsverify.NewStaticResolver())) // placeholder, replaced below
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c := v.TXTChallenge("example.com")

	// Simulate the user publishing exactly the minted record, then verify.
	v2, err := dnsverify.New(dnsverify.WithResolver(
		dnsverify.NewStaticResolver(dnsverify.WithTXT(c.Host, c.Expect[0])),
	))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	res, err := v2.Verify(context.Background(), c)
	if err != nil || !res.Verified {
		t.Fatalf("round-trip: want verified, got %+v err=%v", res, err)
	}
}
