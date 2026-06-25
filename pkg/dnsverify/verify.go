package dnsverify

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
)

var (
	ErrDNSLookupFailed   = errors.New("dns lookup failed")
	ErrDomainNotVerified = errors.New("domain not verified")
	ErrTXTRecordNotFound = errors.New("txt record not found")
	ErrDomainNotFound    = errors.New("domain not found")
	ErrInvalidInput      = errors.New("invalid domain or project id")
)

// Resolver is the minimal DNS resolver seam used by VerifyDomainOwnership.
// *net.Resolver satisfies this interface, and tests can supply a fake to
// exercise the verification logic without touching real DNS.
type Resolver interface {
	LookupTXT(ctx context.Context, name string) ([]string, error)
}

// Compile-time assertion that the stdlib resolver satisfies the seam.
var _ Resolver = (*net.Resolver)(nil)

// VerifyDomainOwnership checks whether the domain publishes a TXT record whose
// value exactly matches projectID. It returns nil on success, or one of the
// package's sentinel errors on failure.
//
// The lookup uses the default system resolver. Use VerifyDomainOwnershipWith to
// supply a custom Resolver (e.g. in tests).
func VerifyDomainOwnership(ctx context.Context, domain, projectID string) error {
	return VerifyDomainOwnershipWith(ctx, &net.Resolver{}, domain, projectID)
}

// VerifyDomainOwnershipWith behaves like VerifyDomainOwnership but performs the
// TXT lookup through the supplied Resolver. A nil resolver falls back to the
// default system resolver.
//
// Normalization rationale:
//   - The domain is lowercased and trimmed because DNS domain names are
//     case-insensitive.
//   - The projectID is only trimmed, never lowercased: TXT record values are
//     case-sensitive, and project IDs (generated via pkg/id) use a
//     case-sensitive alphabet, so lowercasing would break legitimate matches.
//     The same trim-only treatment is applied to each TXT record before the
//     comparison, so an exact, case-sensitive match is required.
func VerifyDomainOwnershipWith(ctx context.Context, resolver Resolver, domain, projectID string) error {
	// Normalize before validating so whitespace-only input is rejected rather
	// than silently passing an empty token to the matcher (verification bypass).
	domain = strings.ToLower(strings.TrimSpace(domain))
	projectID = strings.TrimSpace(projectID)

	if domain == "" || projectID == "" {
		return ErrInvalidInput
	}

	if resolver == nil {
		resolver = &net.Resolver{}
	}

	records, err := resolver.LookupTXT(ctx, domain)
	if err != nil {
		// Distinguish "the domain itself does not exist" (NXDOMAIN) from a
		// transient/network lookup failure. A missing domain can never be
		// verified, so it gets its own sentinel. ErrTXTRecordNotFound is joined
		// for backward compatibility: callers that historically matched the
		// NXDOMAIN case via errors.Is(err, ErrTXTRecordNotFound) keep working,
		// while the finer ErrDomainNotFound is now also available.
		if dnsErr, ok := errors.AsType[*net.DNSError](err); ok && dnsErr.IsNotFound {
			return errors.Join(ErrDomainNotFound, ErrTXTRecordNotFound)
		}
		return fmt.Errorf("%w: %w", ErrDNSLookupFailed, err)
	}

	// The domain resolved but published no TXT records.
	if len(records) == 0 {
		return ErrTXTRecordNotFound
	}

	for _, record := range records {
		if strings.TrimSpace(record) == projectID {
			return nil // Exact match: ownership verified.
		}
	}

	return ErrDomainNotVerified
}
