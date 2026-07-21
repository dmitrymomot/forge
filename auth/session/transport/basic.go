package transport

import (
	"net/http"
	"time"
)

// BasicTransport reads the token from the password slot of HTTP Basic auth
// ("curl -u user:TOKEN"). Build it with Basic.
type BasicTransport struct {
	username string
}

// BasicOption configures a BasicTransport.
type BasicOption func(*BasicTransport)

// WithBasicUsername requires the Basic username to match exactly; requests
// with any other username extract nothing. Without it the username is
// ignored.
func WithBasicUsername(u string) BasicOption {
	return func(b *BasicTransport) { b.username = u }
}

// Basic returns the extraction-only transport for curl-friendly APIs and
// legacy integrations: the session token rides the Basic-auth password slot.
// The credential is client-managed, so Embed and Clear are no-ops — deliver
// the token out of band (the login response body) and treat Destroy as pure
// server-side revocation.
func Basic(opts ...BasicOption) *BasicTransport {
	b := &BasicTransport{}
	for _, opt := range opts {
		opt(b)
	}
	return b
}

// Extract returns the Basic-auth password, or "" when absent (or when a
// required username does not match).
func (b *BasicTransport) Extract(r *http.Request) string {
	user, pass, ok := r.BasicAuth()
	if !ok || (b.username != "" && user != b.username) {
		return ""
	}
	return pass
}

// Embed is a no-op: Basic credentials are stored by the client.
func (b *BasicTransport) Embed(http.ResponseWriter, string, time.Time) error { return nil }

// Clear is a no-op: Basic credentials are stored by the client.
func (b *BasicTransport) Clear(http.ResponseWriter) error { return nil }
