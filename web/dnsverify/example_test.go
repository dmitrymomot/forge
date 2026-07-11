package dnsverify_test

import (
	"context"
	"fmt"

	"github.com/dmitrymomot/forge/web/dnsverify"
)

func Example() {
	// A resolver double stands in for real DNS so the example is deterministic.
	v, err := dnsverify.New(dnsverify.WithResolver(
		dnsverify.NewStaticResolver(
			dnsverify.WithTXT("_forge-verify.example.com", "forge-verification=abc123"),
		),
	))
	if err != nil {
		panic(err)
	}

	// In production, mint a token and persist the Challenge (e.g. in Postgres):
	//   c := v.TXTChallenge("example.com"); save(c)
	// then reload it later and verify. Here we verify a known record.
	c := dnsverify.Challenge{
		Record: dnsverify.TXT,
		Host:   "_forge-verify.example.com",
		Expect: []string{"forge-verification=abc123"},
	}
	res, err := v.Verify(context.Background(), c)
	fmt.Println(res.Verified, err)
	// Output: true <nil>
}
