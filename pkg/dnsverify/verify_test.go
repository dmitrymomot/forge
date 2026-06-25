package dnsverify_test

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/pkg/dnsverify"
)

// fakeResolver is an in-memory Resolver used to exercise the verification logic
// without touching real DNS.
type fakeResolver struct {
	records []string
	err     error
	// gotName captures the name passed to LookupTXT so tests can assert
	// normalization behavior.
	gotName string
}

func (f *fakeResolver) LookupTXT(_ context.Context, name string) ([]string, error) {
	f.gotName = name
	if f.err != nil {
		return nil, f.err
	}
	return f.records, nil
}

func TestVerifyDomainOwnership_InvalidInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		domain    string
		projectID string
	}{
		{name: "both empty", domain: "", projectID: ""},
		{name: "empty domain", domain: "", projectID: "proj-123"},
		{name: "empty projectID", domain: "example.com", projectID: ""},
		{name: "whitespace domain", domain: "   ", projectID: "proj-123"},
		{name: "whitespace projectID", domain: "example.com", projectID: "   "},
		{name: "tab/newline projectID", domain: "example.com", projectID: "\t\n "},
		{name: "both whitespace", domain: " \t ", projectID: " \n "},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// A whitespace-only projectID must never produce a false-positive
			// success even if the resolver returns matching-looking records.
			res := &fakeResolver{records: []string{"proj-123", "", "   "}}
			err := dnsverify.VerifyDomainOwnershipWith(context.Background(), res, tc.domain, tc.projectID)
			require.ErrorIs(t, err, dnsverify.ErrInvalidInput)
			// Lookup must be short-circuited before hitting the resolver.
			require.Empty(t, res.gotName, "resolver should not be called on invalid input")
		})
	}
}

func TestVerifyDomainOwnership_DomainNotFound(t *testing.T) {
	t.Parallel()

	res := &fakeResolver{err: &net.DNSError{Err: "no such host", Name: "example.com", IsNotFound: true}}
	err := dnsverify.VerifyDomainOwnershipWith(context.Background(), res, "example.com", "proj-123")

	require.ErrorIs(t, err, dnsverify.ErrDomainNotFound)
	// NXDOMAIN must not be reported as a generic lookup failure.
	require.NotErrorIs(t, err, dnsverify.ErrDNSLookupFailed)
	// For backward compatibility the NXDOMAIN case is joined with
	// ErrTXTRecordNotFound, so callers matching either sentinel keep working.
	require.ErrorIs(t, err, dnsverify.ErrTXTRecordNotFound)
}

func TestVerifyDomainOwnership_LookupError(t *testing.T) {
	t.Parallel()

	t.Run("generic network error", func(t *testing.T) {
		t.Parallel()

		underlying := errors.New("connection refused")
		res := &fakeResolver{err: underlying}
		err := dnsverify.VerifyDomainOwnershipWith(context.Background(), res, "example.com", "proj-123")

		require.ErrorIs(t, err, dnsverify.ErrDNSLookupFailed)
		// Underlying error chain must be preserved (%w, not %v).
		require.ErrorIs(t, err, underlying)
		require.NotErrorIs(t, err, dnsverify.ErrDomainNotFound)
	})

	t.Run("temporary DNSError without IsNotFound", func(t *testing.T) {
		t.Parallel()

		underlying := &net.DNSError{Err: "server misbehaving", Name: "example.com", IsTemporary: true}
		res := &fakeResolver{err: underlying}
		err := dnsverify.VerifyDomainOwnershipWith(context.Background(), res, "example.com", "proj-123")

		require.ErrorIs(t, err, dnsverify.ErrDNSLookupFailed)
		require.ErrorIs(t, err, underlying)
		// A non-NXDOMAIN DNSError must not be classified as domain-not-found.
		require.NotErrorIs(t, err, dnsverify.ErrDomainNotFound)
	})
}

func TestVerifyDomainOwnership_NoTXTRecords(t *testing.T) {
	t.Parallel()

	t.Run("nil records", func(t *testing.T) {
		t.Parallel()

		res := &fakeResolver{records: nil}
		err := dnsverify.VerifyDomainOwnershipWith(context.Background(), res, "example.com", "proj-123")
		require.ErrorIs(t, err, dnsverify.ErrTXTRecordNotFound)
		require.NotErrorIs(t, err, dnsverify.ErrDomainNotFound)
	})

	t.Run("empty slice", func(t *testing.T) {
		t.Parallel()

		res := &fakeResolver{records: []string{}}
		err := dnsverify.VerifyDomainOwnershipWith(context.Background(), res, "example.com", "proj-123")
		require.ErrorIs(t, err, dnsverify.ErrTXTRecordNotFound)
	})
}

func TestVerifyDomainOwnership_Success(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		records   []string
		projectID string
	}{
		{
			name:      "single exact match",
			records:   []string{"proj-123"},
			projectID: "proj-123",
		},
		{
			name:      "match among multiple records",
			records:   []string{"v=spf1 -all", "google-site-verification=abc", "proj-123"},
			projectID: "proj-123",
		},
		{
			name:      "record has surrounding whitespace",
			records:   []string{"  proj-123  "},
			projectID: "proj-123",
		},
		{
			name:      "projectID has surrounding whitespace",
			records:   []string{"proj-123"},
			projectID: "  proj-123  ",
		},
		{
			name:      "domain normalized but projectID case preserved",
			records:   []string{"Proj-ABC-123"},
			projectID: "Proj-ABC-123",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			res := &fakeResolver{records: tc.records}
			err := dnsverify.VerifyDomainOwnershipWith(context.Background(), res, "Example.COM", tc.projectID)
			require.NoError(t, err)
			// Domain must be normalized (trimmed + lowercased) before lookup.
			require.Equal(t, "example.com", res.gotName)
		})
	}
}

func TestVerifyDomainOwnership_NotVerified(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		records   []string
		projectID string
	}{
		{
			name:      "substring is not an exact match",
			records:   []string{"prefix-proj-123-suffix"},
			projectID: "proj-123",
		},
		{
			name:      "projectID is a substring of a record token",
			records:   []string{"proj-1234"},
			projectID: "proj-123",
		},
		{
			name:      "record is a substring of projectID",
			records:   []string{"proj-12"},
			projectID: "proj-123",
		},
		{
			name:      "case mismatch (projectID is case-sensitive)",
			records:   []string{"proj-abc"},
			projectID: "PROJ-ABC",
		},
		{
			name:      "no matching record among several",
			records:   []string{"v=spf1 -all", "other-token", "yet-another"},
			projectID: "proj-123",
		},
		{
			name:      "internal whitespace differs",
			records:   []string{"proj 123"},
			projectID: "proj-123",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			res := &fakeResolver{records: tc.records}
			err := dnsverify.VerifyDomainOwnershipWith(context.Background(), res, "example.com", tc.projectID)
			require.ErrorIs(t, err, dnsverify.ErrDomainNotVerified)
		})
	}
}

func TestVerifyDomainOwnership_NilResolverFallsBack(t *testing.T) {
	t.Parallel()

	// Passing a nil resolver must fall back to the default system resolver
	// rather than panic. Invalid input still short-circuits before any lookup,
	// so this exercises the fallback path deterministically without real DNS.
	err := dnsverify.VerifyDomainOwnershipWith(context.Background(), nil, "", "proj-123")
	require.ErrorIs(t, err, dnsverify.ErrInvalidInput)
}

func TestVerifyDomainOwnership_DefaultResolverWrapper(t *testing.T) {
	t.Parallel()

	// The exported convenience wrapper must apply the same input validation as
	// the explicit-resolver variant without touching the network.
	err := dnsverify.VerifyDomainOwnership(context.Background(), "  ", "proj-123")
	require.ErrorIs(t, err, dnsverify.ErrInvalidInput)
}
