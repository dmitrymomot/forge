package transport

import (
	"net/http"
	"time"
)

// CookieTransport carries the token in an HttpOnly cookie. Build it with
// Cookie.
type CookieTransport struct {
	name     string
	path     string
	domain   string
	sameSite http.SameSite
	secure   bool
	httpOnly bool
}

// CookieOption configures a CookieTransport.
type CookieOption func(*CookieTransport)

// WithCookieName overrides the cookie name (default "session").
func WithCookieName(name string) CookieOption { return func(c *CookieTransport) { c.name = name } }

// WithCookiePath overrides the cookie path (default "/").
func WithCookiePath(p string) CookieOption { return func(c *CookieTransport) { c.path = p } }

// WithCookieDomain sets an explicit cookie domain (default host-only).
func WithCookieDomain(d string) CookieOption { return func(c *CookieTransport) { c.domain = d } }

// WithCookieSameSite overrides the SameSite attribute (default Lax).
func WithCookieSameSite(s http.SameSite) CookieOption {
	return func(c *CookieTransport) { c.sameSite = s }
}

// WithCookieSecure toggles the Secure attribute — disable only for local
// plain-HTTP development.
func WithCookieSecure(v bool) CookieOption { return func(c *CookieTransport) { c.secure = v } }

// Cookie returns the browser-default transport: an HttpOnly, Secure,
// SameSite=Lax cookie named "session" scoped to "/". The cookie's Expires
// mirrors the session deadline, so the browser drops it when the server
// would refuse it anyway.
func Cookie(opts ...CookieOption) *CookieTransport {
	c := &CookieTransport{
		name:     "session",
		path:     "/",
		sameSite: http.SameSiteLaxMode,
		secure:   true,
		httpOnly: true,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Extract returns the cookie's value, or "" when absent.
func (c *CookieTransport) Extract(r *http.Request) string {
	ck, err := r.Cookie(c.name)
	if err != nil {
		return ""
	}
	return ck.Value
}

// Embed sets the cookie carrying token, expiring at expiresAt.
func (c *CookieTransport) Embed(w http.ResponseWriter, token string, expiresAt time.Time) error {
	http.SetCookie(w, c.cookie(token, expiresAt))
	return nil
}

// Clear expires the cookie immediately.
func (c *CookieTransport) Clear(w http.ResponseWriter) error {
	ck := c.cookie("", time.Time{})
	ck.MaxAge = -1
	http.SetCookie(w, ck)
	return nil
}

func (c *CookieTransport) cookie(value string, expires time.Time) *http.Cookie {
	return &http.Cookie{
		Name:     c.name,
		Value:    value,
		Path:     c.path,
		Domain:   c.domain,
		Expires:  expires,
		SameSite: c.sameSite,
		Secure:   c.secure,
		HttpOnly: c.httpOnly,
	}
}
