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
// # Error Handling
//
// The package provides several specific error types for different verification failures:
//
//   - ErrInvalidInput: domain or projectID is empty
//   - ErrTXTRecordNotFound: no TXT records found for the domain
//   - ErrDNSLookupFailed: DNS lookup encountered a network error
//   - ErrDomainNotVerified: TXT records exist but do not contain the projectID
//
// # Implementation Details
//
// The verification process:
//
// 1. Validates that both domain and projectID are provided
// 2. Normalizes the domain (lowercases, trims whitespace)
// 3. Performs a DNS TXT record lookup for the domain
// 4. Checks if any TXT record contains the projectID string
//
// Domain owners should add a TXT record to their DNS configuration containing
// the project ID. For example:
//
//	example.com TXT "my-project-id-123"
package dnsverify
