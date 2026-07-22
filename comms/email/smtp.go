package email

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"net/textproto"
	"time"
)

// SMTP submits messages to one SMTP server over stdlib net/smtp. It holds no
// connection state — each Send dials, delivers, and quits — so a single
// value is safe for concurrent use. Per-tenant sender identities are
// per-tenant SMTP values (or one shared server with Message.From set per
// send); there is no tenant seam because the sender holds no tenant data.
type SMTP struct {
	auth      smtp.Auth
	tlsConfig *tls.Config
	addr      string
	host      string
	hello     string
	from      string
	tlsMode   TLSMode
	timeout   time.Duration
}

var _ Sender = (*SMTP)(nil)

// New validates cfg and returns an SMTP sender. See Config for the
// env-loadable knobs and Option for the TLS and auth seams.
func New(cfg Config, opts ...Option) (*SMTP, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	var c config
	for _, o := range opts {
		o(&c)
	}
	host, _, err := net.SplitHostPort(cfg.Addr)
	if err != nil {
		return nil, fmt.Errorf("%w: Addr %q: %v", ErrInvalidConfig, cfg.Addr, err)
	}
	auth := c.auth
	if auth == nil && cfg.Username != "" {
		auth = smtp.PlainAuth("", cfg.Username, cfg.Password, host)
	}
	tc := c.tlsConfig.Clone() // nil-receiver Clone returns nil
	if tc == nil {
		tc = &tls.Config{}
	}
	if tc.ServerName == "" {
		tc.ServerName = host
	}
	hello := cfg.Hello
	if hello == "" {
		hello = "localhost"
	}
	return &SMTP{
		auth:      auth,
		tlsConfig: tc,
		addr:      cfg.Addr,
		host:      host,
		hello:     hello,
		from:      cfg.From,
		tlsMode:   cfg.TLS,
		timeout:   cfg.Timeout,
	}, nil
}

// Send validates msg and delivers it in one SMTP session. A message with an
// empty From uses Config.From. The whole session is bounded by Config.Timeout
// (or a sooner ctx deadline). Server rejections map onto a queue's retry
// decision: ErrTransient for 4xx, ErrPermanent for 5xx. A failure after the
// server accepted the message body (the end-of-DATA 250) is not reported as
// an error — the mail is already in flight, and erroring would make a retry
// send it twice.
func (s *SMTP) Send(ctx context.Context, msg Message) error {
	if s == nil || s.addr == "" { // zero SMTP bypassed New
		return errors.New("email: sender not constructed with New")
	}
	if msg.From == "" {
		msg.From = s.from
	}
	env, err := msg.validate()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", s.addr)
	if err != nil {
		return fmt.Errorf("email: dial %s: %w", s.addr, err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	if s.tlsMode == TLSImplicit {
		tconn := tls.Client(conn, s.tlsConfig)
		if err := tconn.HandshakeContext(ctx); err != nil {
			_ = conn.Close()
			return fmt.Errorf("email: tls handshake: %w", err)
		}
		conn = tconn
	}
	client, err := smtp.NewClient(conn, s.host)
	if err != nil {
		_ = conn.Close()
		return classify("greeting", err)
	}
	defer func() { _ = client.Close() }()

	if err := client.Hello(s.hello); err != nil {
		return classify("hello", err)
	}
	if s.tlsMode == TLSStartTLS {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return ErrTLSUnavailable
		}
		if err := client.StartTLS(s.tlsConfig); err != nil {
			return classify("starttls", err)
		}
	}
	if s.auth != nil {
		if err := client.Auth(s.auth); err != nil {
			return classify("auth", err)
		}
	}
	if err := client.Mail(env.from.Address); err != nil {
		return classify("mail", err)
	}
	for _, rcpt := range env.rcpts {
		if err := client.Rcpt(rcpt); err != nil {
			return classify("rcpt "+rcpt, err)
		}
	}
	w, err := client.Data()
	if err != nil {
		return classify("data", err)
	}
	if err := msg.encode(w, env, time.Now()); err != nil {
		_ = w.Close()
		return err
	}
	if err := w.Close(); err != nil {
		return classify("data close", err)
	}
	// The message is accepted; a failed QUIT can't unsend it.
	_ = client.Quit()
	return nil
}

// classify maps an SMTP reply onto the retry-decision sentinels: 4xx is
// transient, 5xx is permanent. Transport errors (timeouts, resets) pass
// through wrapped but unclassified.
func classify(op string, err error) error {
	if te, ok := errors.AsType[*textproto.Error](err); ok {
		switch {
		case te.Code >= 400 && te.Code < 500:
			return fmt.Errorf("%w: %s: %v", ErrTransient, op, err)
		case te.Code >= 500:
			return fmt.Errorf("%w: %s: %v", ErrPermanent, op, err)
		}
	}
	return fmt.Errorf("email: %s: %w", op, err)
}
