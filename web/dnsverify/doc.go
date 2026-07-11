// Package dnsverify performs single-shot DNS ownership and routing
// verification behind a small Resolver seam that *net.Resolver satisfies.
//
// Two intents share one mechanic — look up a record at a host, compare the
// observed value(s) against expected:
//
//   - Ownership token: TXTChallenge mints a random token; the domain owner
//     publishes a TXT record; Verify confirms the token is present.
//   - Routing target: CNAMEChallenge / AChallenge / AAAAChallenge check that a
//     domain points at your ingress (for custom-domain onboarding with
//     hostrouter + autocert).
//
// The package is stateless: TXTChallenge returns a plain, serializable
// Challenge that you persist (e.g. a Postgres row) between showing setup
// instructions and verifying later — nothing needs to survive a restart on
// this side. Verify is single-shot; the caller owns polling cadence (a
// scheduler/jobqueue re-checking a pending domain).
//
// A nil error from Verify has three states: verified (Result.Verified),
// pending (!Verified && len(Found) == 0 — not published yet), and
// misconfigured (!Verified && len(Found) > 0 — published but wrong). A genuine
// resolver failure returns ErrLookup, distinct from "not published yet".
//
//	v, err := dnsverify.New()
//	if err != nil { /* invalid config */ }
//
//	// Issue an ownership challenge and show the record to the user.
//	c := v.TXTChallenge("example.com")
//	// persist c (c.Host, c.Record, c.Expect) tied to the tenant/domain
//
//	// Later, after the user says they added it:
//	res, err := v.Verify(ctx, c)
//	switch {
//	case err != nil:                 // errors.Is(err, dnsverify.ErrLookup)
//	case res.Verified:               // done
//	case len(res.Found) == 0:        // pending — ask them to wait for DNS
//	default:                         // misconfigured — show res.Found
//	}
//
// Consumers render setup instructions from the structured Challenge fields via
// their own i18n layer; dnsverify emits no user-facing prose. The exported
// StaticResolver is an in-memory test double for exercising these flows
// without real DNS.
package dnsverify
