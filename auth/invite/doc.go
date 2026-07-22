// Package invite issues and redeems token invitations into a tenant:
// email-addressed, single-use, expiring invites carrying an opaque role
// payload the package never interprets. Tokens follow the apikey
// discipline — inv_-prefixed secrets with a CRC32 checksum that rejects
// malformed input before any store access, SHA-256 hashes at rest, and
// the plaintext returned exactly once at creation (and on resend, which
// rotates the token). Accepting is constant-time and atomically
// single-use: exactly one Accept wins, replays report ErrAlreadyAccepted.
//
// The package stops at the verified claim: Accept returns the {tenant,
// email, role} the token proves, and membership creation, seat limits,
// and the email send itself (comms/email) stay consumer-side.
//
//	store := invite.NewMemoryStore() // pgstore.New(pool) in production
//	mgr := invite.New(store, invite.WithTTL(72*time.Hour))
//
//	inv, plaintext, err := mgr.Create(ctx, invite.CreateParams{
//		Email: "new.hire@example.com", Tenant: "org_7", Role: "editor",
//	})
//	// Email a link carrying plaintext — only its SHA-256 is stored.
//
//	claim, err := mgr.Accept(ctx, plaintext)
//	// claim == {Tenant: "org_7", Email: "new.hire@example.com", Role: "editor", ...}
//	// Now create the membership in your own tables.
//
// Peek verifies without consuming — serve it on GET so email scanners
// that prefetch links cannot burn the invite, and to render the "join
// org" page before the user commits.
//
// Multi-tenant applications confine management operations with WithScope;
// Accept and Peek need no hook because the invitee typically has no
// tenant context yet — the invite record itself carries the tenant:
//
//	mgr := invite.New(store, invite.WithScope(func(ctx context.Context) (string, error) {
//		return tenantFromCtx(ctx), nil // fail-closed: empty or error aborts
//	}))
//
// Domain auto-join and shareable multi-use links are out of scope.
package invite
