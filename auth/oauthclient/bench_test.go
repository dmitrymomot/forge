package oauthclient_test

import (
	"context"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/oauthclient"
)

func BenchmarkAuthURL(b *testing.B) {
	c, err := oauthclient.New(testKeyset(b),
		oauthclient.WithRedirectURL("https://app.example.com/cb"),
		oauthclient.WithProvider("idp", oidcProvider()))
	require.NoError(b, err)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := c.AuthURL(ctx, "idp"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFlowRoundTrip(b *testing.B) {
	// Seal + parse cost of the flow blob: AuthURL then Exchange up to the
	// state check (the exchange fails on the fabricated callback state —
	// that's fine; the sealed-blob parse is the hot part being measured).
	c, err := oauthclient.New(testKeyset(b),
		oauthclient.WithRedirectURL("https://app.example.com/cb"),
		oauthclient.WithProvider("idp", oidcProvider()))
	require.NoError(b, err)
	ctx := context.Background()
	flow, err := c.AuthURL(ctx, "idp")
	require.NoError(b, err)
	cb := url.Values{"code": {"c"}, "state": {"wrong"}}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = c.Exchange(ctx, flow.FlowToken, cb)
	}
}
