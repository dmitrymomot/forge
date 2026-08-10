// Package magiclink issues and redeems signed, TTL'd, optionally single-use
// links: passwordless login, team invites, email verification, and
// unsubscribe flows. It builds on crypto/token for the token format and adds
// single-use redemption over the resilience/cache Store seam, tenant-scope
// binding, and URL construction. It does not send email — delivery is the
// caller's channel.
//
// Stateless by default: without WithStore a link verifies until its TTL
// expires and may be redeemed repeatedly (fine for unsubscribe and verify
// flows where replay is harmless). WithStore makes Redeem atomically
// single-use. The bundled LRU memory store can evict live keys under
// pressure; production single-use needs a durable Store the consumer
// supplies.
//
// Email scanners (Outlook SafeLinks and friends) prefetch links before the
// user clicks. Serve GET with Peek — it verifies without consuming — and
// consume with Redeem on an explicit POST:
//
//	type LoginClaims struct {
//		UserID string `json:"uid"`
//	}
//
//	links, err := magiclink.New[LoginClaims](key, "login",
//		magiclink.WithTTL(15*time.Minute),
//		magiclink.WithStore(store), // single-use; omit for stateless links
//		magiclink.WithBaseURL("https://app.example.com/auth/verify"),
//	)
//
//	// Request: build the link and deliver it yourself.
//	url, err := links.IssueURL(ctx, "", LoginClaims{UserID: "u_1"})
//
//	// GET /auth/verify?token=... — show a confirm button, do not consume.
//	claims, err := links.Peek(r.Context(), r.URL.Query().Get("token"))
//
//	// POST /auth/verify — consume; a second redeem returns ErrUsed.
//	claims, err = links.Redeem(r.Context(), r.FormValue("token"))
//
// Errors are matched with errors.Is: ErrInvalid (malformed, bad signature,
// wrong purpose), ErrExpired, ErrUsed, ErrScopeMismatch, and ErrStore (store
// failure; redemption fails closed).
//
// The token arrives as attacker-controlled input before signature
// verification, and verification cost is linear in its length. Peek and Redeem
// reject links over a generous default cap (8192 bytes) before any decode as
// defense-in-depth; tune it with WithMaxTokenLength, and still prefer bounding
// the token query-parameter/form-field size at the HTTP layer.
//
// Multi-tenant apps bind links to a tenant with WithScope: Issue stamps the
// scope resolved from ctx into the token, Peek/Redeem recompute it and fail
// closed on mismatch. A link issued with an empty scope is global and
// redeems in any tenant context — a hook that wants to forbid global
// issuance returns an error when ctx lacks a tenant. White-label callers
// pass the tenant's domain as IssueURL's base argument:
//
//	invites, err := magiclink.New[InviteClaims](key, "invite",
//		magiclink.WithStore(store),
//		magiclink.WithScope(func(ctx context.Context) (string, error) {
//			return tenant.FromContext(ctx), nil // "" = global link
//		}),
//	)
//	url, err := invites.IssueURL(ctx, "https://acme.example.com/join",
//		InviteClaims{Email: "new@hire.com", Role: "admin"})
package magiclink
