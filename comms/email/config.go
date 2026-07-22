package email

import (
	"fmt"
	"net"
	"net/mail"
	"time"
)

// TLSMode selects how the SMTP connection is secured.
type TLSMode string

const (
	// TLSStartTLS (the default) connects in plaintext and upgrades via
	// STARTTLS before any credentials or mail flow. The upgrade is mandatory:
	// a server that does not advertise STARTTLS fails the send.
	TLSStartTLS TLSMode = "starttls"

	// TLSImplicit performs a TLS handshake immediately on connect — the
	// SMTPS convention, usually port 465.
	TLSImplicit TLSMode = "implicit"

	// TLSNone sends over plaintext. For local development catchers
	// (Mailpit, MailHog) only; net/smtp refuses PLAIN auth over plaintext to
	// any host but localhost.
	TLSNone TLSMode = "none"
)

// Config is the env-loadable SMTP sender configuration. The canonical loading
// flow preserves defaults:
//
//	cfg := email.DefaultConfig()
//	err := appconfig.Populate(&cfg)
type Config struct {
	// Addr is the server's host:port, e.g. "smtp.example.com:587".
	Addr string `env:"EMAIL_SMTP_ADDR"`
	// Username and Password enable PLAIN authentication when both are set.
	Username string `env:"EMAIL_SMTP_USERNAME"`
	Password string `env:"EMAIL_SMTP_PASSWORD"`
	// TLS is the connection security mode (default TLSStartTLS).
	TLS TLSMode `env:"EMAIL_SMTP_TLS"`
	// Hello overrides the EHLO hostname (default "localhost").
	Hello string `env:"EMAIL_SMTP_HELLO"`
	// From, when set, fills Message.From for messages that leave it empty —
	// the app-wide sender identity.
	From string `env:"EMAIL_FROM"`
	// Timeout bounds one whole Send: dial, handshake, and mail flow
	// (default 15s). A sooner context deadline still wins.
	Timeout time.Duration `env:"EMAIL_TIMEOUT"`
}

// DefaultConfig returns the default policy: mandatory STARTTLS and a 15s
// send timeout.
func DefaultConfig() Config {
	return Config{TLS: TLSStartTLS, Timeout: 15 * time.Second}
}

// Validate checks required fields.
func (c Config) Validate() error {
	host, port, err := net.SplitHostPort(c.Addr)
	if err != nil || host == "" || port == "" {
		return fmt.Errorf("%w: Addr must be host:port, got %q", ErrInvalidConfig, c.Addr)
	}
	switch c.TLS {
	case TLSStartTLS, TLSImplicit, TLSNone:
	default:
		return fmt.Errorf("%w: unknown TLS mode %q", ErrInvalidConfig, c.TLS)
	}
	if (c.Username == "") != (c.Password == "") {
		return fmt.Errorf("%w: Username and Password must be set together", ErrInvalidConfig)
	}
	if c.From != "" {
		if _, err := mail.ParseAddress(c.From); err != nil {
			return fmt.Errorf("%w: From address %q: %v", ErrInvalidConfig, c.From, err)
		}
	}
	if c.Timeout <= 0 {
		return fmt.Errorf("%w: Timeout must be positive", ErrInvalidConfig)
	}
	return nil
}
