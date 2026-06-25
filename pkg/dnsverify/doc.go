// Package dnsverify provides DNS-based domain ownership verification.
//
// This package allows applications to verify that a domain owner has control
// over a domain by checking for the presence of a specific project ID in a
// TXT DNS record. This is commonly used for email authentication, domain claims,
// and similar verification workflows.
//
// # Basic Usage
//
// The primary function is VerifyDomainOwnership, which performs the verification:
//
//	import (
//		"context"
//		"github.com/dmitrymomot/forge/pkg/dnsverify"
//	)
//
//	func main() {
//		ctx := context.Background()
//		err := dnsverify.VerifyDomainOwnership(ctx, "example.com", "my-project-id-123")
//		if err != nil {
//			// Handle verification failure
//		}
//	}
//
// # Custom Resolver
//
// VerifyDomainOwnershipWith accepts a Resolver, which is satisfied by
// *net.Resolver and by any test fake. This makes the verification logic
// unit-testable without performing real DNS lookups:
//
//	err := dnsverify.VerifyDomainOwnershipWith(ctx, resolver, "example.com", "my-project-id-123")
//
// # Error Handling
//
// The package provides several specific sentinel errors for different
// verification failures, all matchable via errors.Is:
//
//   - ErrInvalidInput: domain or projectID is empty after trimming whitespace
//   - ErrDomainNotFound: the domain itself does not exist (NXDOMAIN). For
//     backward compatibility this case also matches ErrTXTRecordNotFound, so
//     errors.Is(err, ErrTXTRecordNotFound) is true for both NXDOMAIN and a
//     domain that resolved with no TXT records.
//   - ErrTXTRecordNotFound: the domain resolved but has no TXT records (also
//     matched on NXDOMAIN, see above)
//   - ErrDNSLookupFailed: the DNS lookup failed (e.g. network/timeout); the
//     underlying error is wrapped and remains available via errors.Is/As
//   - ErrDomainNotVerified: TXT records exist but none exactly match the projectID
//
// # Implementation Details
//
// The verification process:
//
//  1. Normalizes the domain (lowercases, trims whitespace) and trims the projectID
//  2. Validates that both domain and projectID are non-empty after normalization
//  3. Performs a DNS TXT record lookup for the domain
//  4. Requires an exact, case-sensitive match between a (trimmed) TXT record and
//     the projectID
//
// The domain is lowercased because DNS names are case-insensitive. The projectID
// is only trimmed, not lowercased, because TXT record values are case-sensitive
// and project IDs use a case-sensitive alphabet; the same trim-only treatment is
// applied to each TXT record so matching remains exact.
//
// Domain owners should add a TXT record to their DNS configuration containing
// the project ID. For example:
//
//	example.com TXT "my-project-id-123"
package dnsverify
