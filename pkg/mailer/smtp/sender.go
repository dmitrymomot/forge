package smtp

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"time"

	"github.com/dmitrymomot/forge/pkg/mailer"
)

// Sender implements mailer.Sender using SMTP.
type Sender struct {
	config Config
}

// New creates a new SMTP sender.
func New(cfg Config) *Sender {
	return &Sender{config: cfg}
}

// Send implements mailer.Sender.
func (s *Sender) Send(ctx context.Context, email *mailer.Email) error {
	from := email.From
	if from == "" {
		if s.config.SenderName != "" {
			from = fmt.Sprintf("%s <%s>", s.config.SenderName, s.config.SenderEmail)
		} else {
			from = s.config.SenderEmail
		}
	}

	msg, err := buildMessage(from, email)
	if err != nil {
		return fmt.Errorf("smtp: build message: %w", err)
	}

	envelope, err := extractEmail(from)
	if err != nil {
		return fmt.Errorf("smtp: invalid from address: %w", err)
	}

	recipients, err := collectRecipients(email)
	if err != nil {
		return err
	}

	addr := net.JoinHostPort(s.config.Host, strconv.Itoa(s.config.Port))

	conn, err := (&net.Dialer{Timeout: 30 * time.Second}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("smtp: dial %s: %w", addr, err)
	}

	client, err := smtp.NewClient(conn, s.config.Host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("smtp: new client: %w", err)
	}
	defer client.Close()

	if s.config.TLS {
		tlsConfig := &tls.Config{ServerName: s.config.Host}
		if err := client.StartTLS(tlsConfig); err != nil {
			return fmt.Errorf("smtp: starttls: %w", err)
		}
	}

	if s.config.Username != "" && s.config.Password != "" {
		auth := smtp.PlainAuth("", s.config.Username, s.config.Password, s.config.Host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("smtp: auth: %w", err)
		}
	}

	if err := client.Mail(envelope); err != nil {
		return fmt.Errorf("smtp: MAIL FROM: %w", err)
	}

	for _, rcpt := range recipients {
		if err := client.Rcpt(rcpt); err != nil {
			return fmt.Errorf("smtp: RCPT TO <%s>: %w", rcpt, err)
		}
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp: DATA: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		return fmt.Errorf("smtp: write message: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp: close data: %w", err)
	}

	return client.Quit()
}

// extractEmail parses an RFC 5322 address and returns just the email part.
func extractEmail(addr string) (string, error) {
	parsed, err := mail.ParseAddress(addr)
	if err != nil {
		return "", fmt.Errorf("smtp: parse address %q: %w", addr, err)
	}
	return parsed.Address, nil
}

// collectRecipients gathers all envelope recipients (To + CC + BCC),
// extracting bare email addresses from RFC 5322 formatted strings.
func collectRecipients(email *mailer.Email) ([]string, error) {
	all := make([]string, 0, len(email.To)+len(email.CC)+len(email.BCC))
	for _, group := range [][]string{email.To, email.CC, email.BCC} {
		for _, addr := range group {
			bare, err := extractEmail(addr)
			if err != nil {
				return nil, err
			}
			all = append(all, bare)
		}
	}
	return all, nil
}
