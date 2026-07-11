package dnsverify_test

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"testing"

	"github.com/dmitrymomot/forge/web/dnsverify"
)

func newVerifier(t *testing.T, r dnsverify.Resolver) *dnsverify.Verifier {
	t.Helper()
	v, err := dnsverify.New(dnsverify.WithResolver(r))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return v
}

func TestVerifyTXT(t *testing.T) {
	v := newVerifier(t, dnsverify.NewStaticResolver(
		dnsverify.WithTXT("_forge-verify.example.com", "other=1", "forge-verification=abc"),
	))
	c := dnsverify.Challenge{
		Record: dnsverify.TXT,
		Host:   "_forge-verify.example.com",
		Expect: []string{"forge-verification=abc"},
	}
	res, err := v.Verify(context.Background(), c)
	if err != nil || !res.Verified {
		t.Fatalf("want verified, got %+v err=%v", res, err)
	}
}

func TestVerifyTXTMisconfigured(t *testing.T) {
	v := newVerifier(t, dnsverify.NewStaticResolver(
		dnsverify.WithTXT("_forge-verify.example.com", "forge-verification=WRONG"),
	))
	c := dnsverify.Challenge{
		Record: dnsverify.TXT,
		Host:   "_forge-verify.example.com",
		Expect: []string{"forge-verification=abc"},
	}
	res, err := v.Verify(context.Background(), c)
	if err != nil || res.Verified || len(res.Found) != 1 {
		t.Fatalf("want misconfigured (found, not verified), got %+v err=%v", res, err)
	}
}

func TestVerifyPendingIsNotError(t *testing.T) {
	v := newVerifier(t, dnsverify.NewStaticResolver()) // nothing published
	c := dnsverify.Challenge{
		Record: dnsverify.TXT,
		Host:   "_forge-verify.example.com",
		Expect: []string{"forge-verification=abc"},
	}
	res, err := v.Verify(context.Background(), c)
	if err != nil || res.Verified || len(res.Found) != 0 {
		t.Fatalf("want pending (nil err, empty found), got %+v err=%v", res, err)
	}
}

func TestVerifyTemporaryErrorIsErrLookup(t *testing.T) {
	temp := &net.DNSError{Err: "server misbehaving", Name: "h", IsTemporary: true}
	v := newVerifier(t, dnsverify.NewStaticResolver(
		dnsverify.WithTXT("h.example.com", "x"),
		dnsverify.WithLookupError("h.example.com", temp),
	))
	c := dnsverify.Challenge{Record: dnsverify.TXT, Host: "h.example.com", Expect: []string{"y"}}
	_, err := v.Verify(context.Background(), c)
	if !errors.Is(err, dnsverify.ErrLookup) {
		t.Fatalf("want ErrLookup, got %v", err)
	}
}

func TestVerifyCNAMENormalizes(t *testing.T) {
	v := newVerifier(t, dnsverify.NewStaticResolver(
		dnsverify.WithCNAME("app.example.com", "Ingress.SVC.com."), // mixed case + trailing dot
	))
	c := dnsverify.Challenge{
		Record: dnsverify.CNAME,
		Host:   "app.example.com",
		Expect: []string{"ingress.svc.com"},
	}
	res, err := v.Verify(context.Background(), c)
	if err != nil || !res.Verified {
		t.Fatalf("want verified (normalized), got %+v err=%v", res, err)
	}
}

func TestVerifyAIntersects(t *testing.T) {
	v := newVerifier(t, dnsverify.NewStaticResolver(
		dnsverify.WithIP("example.com", netip.MustParseAddr("198.51.100.7"), netip.MustParseAddr("203.0.113.10")),
	))
	c := dnsverify.Challenge{
		Record: dnsverify.A,
		Host:   "example.com",
		Expect: []string{"203.0.113.10"}, // one of the resolved set
	}
	res, err := v.Verify(context.Background(), c)
	if err != nil || !res.Verified || len(res.Found) != 2 {
		t.Fatalf("want verified with 2 found, got %+v err=%v", res, err)
	}
}

func TestVerifyInvalidChallenge(t *testing.T) {
	v := newVerifier(t, dnsverify.NewStaticResolver())
	cases := []dnsverify.Challenge{
		{Record: dnsverify.TXT, Host: "", Expect: []string{"x"}},             // empty host
		{Record: dnsverify.TXT, Host: "h", Expect: nil},                      // empty expect
		{Record: dnsverify.RecordType(99), Host: "h", Expect: []string{"x"}}, // unknown record
	}
	for i, c := range cases {
		if _, err := v.Verify(context.Background(), c); !errors.Is(err, dnsverify.ErrInvalidChallenge) {
			t.Errorf("case %d: want ErrInvalidChallenge, got %v", i, err)
		}
	}
}
