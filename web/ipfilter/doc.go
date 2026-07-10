// Package ipfilter provides allow/deny IP middleware over web/clientip for admin
// allowlists, partner IP pinning, and blocklists.
//
// It uses a deny-wins model: a denylist match always blocks; a configured
// allowlist is a default-deny gate (only listed ranges pass); with no allowlist,
// anything not denied passes. The client IP is resolved via clientip.Resolve, so
// proxy/trust settings are explicit via WithClientIP. Blocked requests are
// answered by a problem.Responder (default problem.JSON 403). Invalid CIDRs make
// New panic — they are wiring bugs.
//
//	mux.Handle("/admin/", ipfilter.New(
//		ipfilter.WithAllow("203.0.113.0/24"), // office range
//		ipfilter.WithDeny("203.0.113.66"),    // one compromised host inside it
//		ipfilter.WithClientIP(clientip.Cloudflare()),
//	)(adminHandler))
package ipfilter
