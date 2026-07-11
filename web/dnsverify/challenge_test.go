package dnsverify_test

import (
	"testing"

	"github.com/dmitrymomot/forge/web/dnsverify"
)

func TestRecordTypeString(t *testing.T) {
	cases := map[dnsverify.RecordType]string{
		dnsverify.TXT:   "TXT",
		dnsverify.CNAME: "CNAME",
		dnsverify.A:     "A",
		dnsverify.AAAA:  "AAAA",
	}
	for rt, want := range cases {
		if got := rt.String(); got != want {
			t.Errorf("RecordType(%d).String() = %q, want %q", rt, got, want)
		}
	}
	if got := dnsverify.RecordType(99).String(); got != "UNKNOWN" {
		t.Errorf("unknown RecordType.String() = %q, want UNKNOWN", got)
	}
}

func TestChallengeIsPlainValue(t *testing.T) {
	c := dnsverify.Challenge{
		Record: dnsverify.TXT,
		Host:   "_forge-verify.example.com",
		Expect: []string{"forge-verification=abc"},
	}
	if c.Host != "_forge-verify.example.com" || len(c.Expect) != 1 {
		t.Fatalf("Challenge fields not readable: %+v", c)
	}
}
