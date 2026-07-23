package email

import (
	"crypto/tls"
	"net/smtp"
)

type config struct {
	tlsConfig *tls.Config
	auth      smtp.Auth
}

// Option configures the SMTP sender beyond the env-loadable Config.
type Option func(*config)

// WithTLSConfig replaces the TLS client configuration used for both STARTTLS
// and implicit TLS — the seam for private CAs, a ServerName override, or test
// certificates. When its ServerName is empty the sender fills in the host
// from Addr. Nil is ignored.
func WithTLSConfig(tc *tls.Config) Option {
	return func(c *config) {
		if tc != nil {
			c.tlsConfig = tc
		}
	}
}

// WithAuth replaces the authentication mechanism (default: PLAIN when
// Config.Username is set, none otherwise) — the seam for CRAM-MD5 or a
// provider-specific scheme. Nil is ignored.
func WithAuth(a smtp.Auth) Option {
	return func(c *config) {
		if a != nil {
			c.auth = a
		}
	}
}
