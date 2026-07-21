package transport

import (
	"net/http"
	"strings"
	"time"
)

// defaultTokenHeader is the response header carrying issued/rotated tokens
// for header-based transports.
const defaultTokenHeader = "X-Session-Token"

// BearerTransport reads "Authorization: Bearer <token>" and returns
// issued/rotated tokens in a response header the client persists. Build it
// with Bearer.
type BearerTransport struct {
	header string
}

// BearerOption configures a BearerTransport.
type BearerOption func(*BearerTransport)

// WithBearerResponseHeader overrides the response header carrying new tokens
// (default X-Session-Token).
func WithBearerResponseHeader(name string) BearerOption {
	return func(b *BearerTransport) { b.header = name }
}

// Bearer returns the SPA/mobile transport: tokens arrive as an
// Authorization Bearer credential; Embed answers with the current token in
// the response header (default X-Session-Token) whenever it was minted or
// rotated, and the client replaces its stored copy. Clear sends the header
// empty — the signal to forget the token.
func Bearer(opts ...BearerOption) *BearerTransport {
	b := &BearerTransport{header: defaultTokenHeader}
	for _, opt := range opts {
		opt(b)
	}
	return b
}

// Extract returns the Bearer credential, or "" when absent.
func (b *BearerTransport) Extract(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(auth) <= len(prefix) || !strings.EqualFold(auth[:len(prefix)], prefix) {
		return ""
	}
	return auth[len(prefix):]
}

// Embed writes token to the response header.
func (b *BearerTransport) Embed(w http.ResponseWriter, token string, _ time.Time) error {
	w.Header().Set(b.header, token)
	return nil
}

// Clear writes an empty response header — the client's signal to drop its
// stored token.
func (b *BearerTransport) Clear(w http.ResponseWriter) error {
	w.Header().Set(b.header, "")
	return nil
}
