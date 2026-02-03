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
	ErrInvalidInput      = errors.New("invalid domain or project id")
)

// VerifyDomainOwnership checks if the domain has a TXT record containing the projectID.
// Returns nil if verification succeeds, otherwise returns a specific error.
func VerifyDomainOwnership(ctx context.Context, domain, projectID string) error {
	if domain == "" || projectID == "" {
		return ErrInvalidInput
	}

	// Normalize domain (trim whitespace, lowercase)
	domain = strings.ToLower(strings.TrimSpace(domain))
	projectID = strings.TrimSpace(projectID)

	resolver := &net.Resolver{}
	records, err := resolver.LookupTXT(ctx, domain)
	if err != nil {
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) {
			if dnsErr.IsNotFound {
				return ErrTXTRecordNotFound
			}
		}
		return fmt.Errorf("%w: %v", ErrDNSLookupFailed, err)
	}

	for _, record := range records {
		if strings.Contains(record, projectID) {
			return nil // Success!
		}
	}

	return ErrDomainNotVerified
}
