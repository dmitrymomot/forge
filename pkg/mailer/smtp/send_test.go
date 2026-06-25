package smtp_test

import (
	"bytes"
	"context"
	"net/mail"
	"strings"
	"testing"
	"time"

	smtpmock "github.com/mocktools/go-smtp-mock/v2"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/pkg/mailer"
	"github.com/dmitrymomot/forge/pkg/mailer/smtp"
)

// newTestSMTPServer starts an in-process SMTP mock and returns the server
// plus a Config pointing at it. The server is stopped automatically via t.Cleanup.
func newTestSMTPServer(t *testing.T) (*smtpmock.Server, smtp.Config) {
	t.Helper()
	server := smtpmock.New(smtpmock.ConfigurationAttr{
		MultipleRcptto:           true,
		MultipleMessageReceiving: true,
	})
	require.NoError(t, server.Start())
	t.Cleanup(func() { _ = server.Stop() })
	return server, smtp.Config{
		Host:        "127.0.0.1",
		Port:        server.PortNumber(),
		SenderEmail: "test@example.com",
		SenderName:  "Test Sender",
	}
}

// waitForMessages polls the mock server for the expected number of messages,
// avoiding races between the SMTP session closing and the mock recording the message.
func waitForMessages(t *testing.T, server *smtpmock.Server, count int) []smtpmock.Message {
	t.Helper()
	msgs, err := server.WaitForMessages(count, 5*time.Second)
	require.NoError(t, err, "timed out waiting for %d message(s)", count)
	require.Len(t, msgs, count)
	return msgs
}

func TestSend(t *testing.T) {
	t.Parallel()

	t.Run("sends simple text email", func(t *testing.T) {
		t.Parallel()

		server, cfg := newTestSMTPServer(t)
		sender := smtp.New(cfg)

		err := sender.Send(context.Background(), &mailer.Email{
			To:      []string{"recipient@example.com"},
			Subject: "Test Email",
			Text:    "This is a test message",
		})
		require.NoError(t, err)

		msgs := waitForMessages(t, server, 1)
		require.True(t, msgs[0].IsConsistent())

		body := msgs[0].MsgRequest()
		require.Contains(t, body, "Subject:")
		require.Contains(t, body, "This is a test message")
	})

	t.Run("sends HTML email", func(t *testing.T) {
		t.Parallel()

		server, cfg := newTestSMTPServer(t)
		sender := smtp.New(cfg)

		err := sender.Send(context.Background(), &mailer.Email{
			To:      []string{"user@example.com"},
			Subject: "HTML Test",
			HTML:    "<html><body><h1>Hello</h1></body></html>",
		})
		require.NoError(t, err)

		msgs := waitForMessages(t, server, 1)

		body := msgs[0].MsgRequest()
		require.Contains(t, body, "text/html")
		require.Contains(t, body, "Hello")
	})

	t.Run("sends multipart alternative", func(t *testing.T) {
		t.Parallel()

		server, cfg := newTestSMTPServer(t)
		sender := smtp.New(cfg)

		err := sender.Send(context.Background(), &mailer.Email{
			To:      []string{"user@example.com"},
			Subject: "Multipart Test",
			Text:    "Plain text version",
			HTML:    "<p>HTML version</p>",
		})
		require.NoError(t, err)

		msgs := waitForMessages(t, server, 1)

		body := msgs[0].MsgRequest()
		require.Contains(t, body, "multipart/alternative")
		require.Contains(t, body, "text/plain")
		require.Contains(t, body, "text/html")
	})

	t.Run("sends email with attachment", func(t *testing.T) {
		t.Parallel()

		server, cfg := newTestSMTPServer(t)
		sender := smtp.New(cfg)

		err := sender.Send(context.Background(), &mailer.Email{
			To:      []string{"user@example.com"},
			Subject: "Attachment Test",
			HTML:    "<p>See attached</p>",
			Attachments: []mailer.Attachment{
				{
					Filename:    "test.txt",
					ContentType: "text/plain",
					Content:     []byte("attachment content"),
				},
			},
		})
		require.NoError(t, err)

		msgs := waitForMessages(t, server, 1)

		body := msgs[0].MsgRequest()
		require.Contains(t, body, "multipart/mixed")
		require.Contains(t, body, "test.txt")
	})

	t.Run("sends to multiple recipients", func(t *testing.T) {
		t.Parallel()

		server, cfg := newTestSMTPServer(t)
		sender := smtp.New(cfg)

		err := sender.Send(context.Background(), &mailer.Email{
			To:      []string{"to1@example.com", "to2@example.com"},
			CC:      []string{"cc@example.com"},
			BCC:     []string{"bcc1@example.com", "bcc2@example.com"},
			Subject: "Multiple Recipients",
			Text:    "Hello everyone",
		})
		require.NoError(t, err)

		msgs := waitForMessages(t, server, 1)

		rcpts := msgs[0].RcpttoRequestResponse()
		// 2 To + 1 CC + 2 BCC = 5 recipients
		require.Len(t, rcpts, 5)

		// Collect all RCPT TO addresses
		var addrs []string
		for _, pair := range rcpts {
			addrs = append(addrs, pair[0])
		}
		joined := strings.Join(addrs, " ")
		require.Contains(t, joined, "to1@example.com")
		require.Contains(t, joined, "to2@example.com")
		require.Contains(t, joined, "cc@example.com")
		require.Contains(t, joined, "bcc1@example.com")
		require.Contains(t, joined, "bcc2@example.com")
	})

	t.Run("uses config sender when From is empty", func(t *testing.T) {
		t.Parallel()

		server, cfg := newTestSMTPServer(t)
		sender := smtp.New(cfg)

		err := sender.Send(context.Background(), &mailer.Email{
			To:      []string{"user@example.com"},
			Subject: "Default Sender",
			Text:    "Should use config sender",
		})
		require.NoError(t, err)

		msgs := waitForMessages(t, server, 1)
		require.Contains(t, msgs[0].MailfromRequest(), "test@example.com")
	})

	t.Run("sends when config sender name contains a comma", func(t *testing.T) {
		t.Parallel()

		server, cfg := newTestSMTPServer(t)
		// A display name with a comma must be RFC 5322 quoted, otherwise the
		// address parses as two recipients and sending silently fails.
		cfg.SenderName = "Doe, John"
		cfg.SenderEmail = "john@example.com"
		sender := smtp.New(cfg)

		err := sender.Send(context.Background(), &mailer.Email{
			To:      []string{"user@example.com"},
			Subject: "Comma Name",
			Text:    "should still send",
		})
		require.NoError(t, err)

		msgs := waitForMessages(t, server, 1)

		body := msgs[0].MsgRequest()
		// The display name must be quoted in the From header.
		require.Contains(t, body, `From: "Doe, John" <john@example.com>`)
		// The envelope MAIL FROM must use the bare address.
		require.Contains(t, msgs[0].MailfromRequest(), "john@example.com")

		// Header must be parseable and yield a single, intact From address.
		parsed, rerr := mail.ReadMessage(bytes.NewReader([]byte(body)))
		require.NoError(t, rerr)
		addr, perr := mail.ParseAddress(parsed.Header.Get("From"))
		require.NoError(t, perr)
		require.Equal(t, "Doe, John", addr.Name)
		require.Equal(t, "john@example.com", addr.Address)
	})

	t.Run("uses custom From when provided", func(t *testing.T) {
		t.Parallel()

		server, cfg := newTestSMTPServer(t)
		sender := smtp.New(cfg)

		err := sender.Send(context.Background(), &mailer.Email{
			To:      []string{"user@example.com"},
			Subject: "Custom Sender",
			Text:    "Custom from",
			From:    "Custom Name <custom@example.com>",
		})
		require.NoError(t, err)

		msgs := waitForMessages(t, server, 1)

		body := msgs[0].MsgRequest()
		require.Contains(t, body, "From: Custom Name <custom@example.com>")
		require.Contains(t, msgs[0].MailfromRequest(), "custom@example.com")
	})

	t.Run("includes Date and Message-ID headers", func(t *testing.T) {
		t.Parallel()

		server, cfg := newTestSMTPServer(t)
		sender := smtp.New(cfg)

		err := sender.Send(context.Background(), &mailer.Email{
			To:      []string{"user@example.com"},
			Subject: "Headers Test",
			Text:    "needs Date and Message-ID",
		})
		require.NoError(t, err)

		msgs := waitForMessages(t, server, 1)

		parsed, rerr := mail.ReadMessage(strings.NewReader(msgs[0].MsgRequest()))
		require.NoError(t, rerr)

		dateVal := parsed.Header.Get("Date")
		require.NotEmpty(t, dateVal)
		_, derr := time.Parse(time.RFC1123Z, dateVal)
		require.NoError(t, derr, "Date header %q must be RFC 1123Z", dateVal)

		mid := parsed.Header.Get("Message-ID")
		require.NotEmpty(t, mid)
		require.True(t, strings.HasPrefix(mid, "<") && strings.HasSuffix(mid, ">"),
			"Message-ID %q must be angle-bracket wrapped", mid)
	})

	t.Run("sends with ReplyTo header", func(t *testing.T) {
		t.Parallel()

		server, cfg := newTestSMTPServer(t)
		sender := smtp.New(cfg)

		err := sender.Send(context.Background(), &mailer.Email{
			To:      []string{"user@example.com"},
			Subject: "Reply-To Test",
			Text:    "Reply to different address",
			ReplyTo: "replies@example.com",
		})
		require.NoError(t, err)

		msgs := waitForMessages(t, server, 1)
		require.Contains(t, msgs[0].MsgRequest(), "Reply-To: replies@example.com")
	})

	t.Run("sends with custom headers", func(t *testing.T) {
		t.Parallel()

		server, cfg := newTestSMTPServer(t)
		sender := smtp.New(cfg)

		err := sender.Send(context.Background(), &mailer.Email{
			To:      []string{"user@example.com"},
			Subject: "Custom Headers",
			Text:    "Has custom headers",
			Headers: map[string]string{
				"X-Priority":    "1",
				"X-Custom-Flag": "integration-test",
			},
		})
		require.NoError(t, err)

		msgs := waitForMessages(t, server, 1)

		body := msgs[0].MsgRequest()
		require.Contains(t, body, "X-Priority: 1")
		require.Contains(t, body, "X-Custom-Flag: integration-test")
	})

	t.Run("context cancellation returns error", func(t *testing.T) {
		t.Parallel()

		_, cfg := newTestSMTPServer(t)
		sender := smtp.New(cfg)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := sender.Send(ctx, &mailer.Email{
			To:      []string{"user@example.com"},
			Subject: "Should Not Send",
			Text:    "cancelled",
		})
		require.Error(t, err)
		require.ErrorIs(t, ctx.Err(), context.Canceled)
	})

	t.Run("context timeout returns error", func(t *testing.T) {
		t.Parallel()

		_, cfg := newTestSMTPServer(t)
		sender := smtp.New(cfg)

		ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
		defer cancel()
		time.Sleep(10 * time.Millisecond)

		err := sender.Send(ctx, &mailer.Email{
			To:      []string{"user@example.com"},
			Subject: "Should Timeout",
			Text:    "timed out",
		})
		require.Error(t, err)
		require.ErrorIs(t, ctx.Err(), context.DeadlineExceeded)
	})

	t.Run("invalid From address returns error", func(t *testing.T) {
		t.Parallel()

		_, cfg := newTestSMTPServer(t)
		sender := smtp.New(cfg)

		err := sender.Send(context.Background(), &mailer.Email{
			To:      []string{"user@example.com"},
			Subject: "Invalid From",
			Text:    "should fail",
			From:    "not-an-email",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid from address")
	})

	t.Run("invalid recipient returns error", func(t *testing.T) {
		t.Parallel()

		_, cfg := newTestSMTPServer(t)
		sender := smtp.New(cfg)

		err := sender.Send(context.Background(), &mailer.Email{
			To:      []string{"invalid-email"},
			Subject: "Invalid Recipient",
			Text:    "should fail",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "parse address")
	})

	t.Run("non-ASCII subject is MIME encoded", func(t *testing.T) {
		t.Parallel()

		server, cfg := newTestSMTPServer(t)
		sender := smtp.New(cfg)

		err := sender.Send(context.Background(), &mailer.Email{
			To:      []string{"user@example.com"},
			Subject: "Тест UTF-8 中文 🎉",
			Text:    "UTF-8 subject test",
		})
		require.NoError(t, err)

		msgs := waitForMessages(t, server, 1)

		body := msgs[0].MsgRequest()
		// Non-ASCII subjects get Q-encoded
		require.Contains(t, body, "=?utf-8?")
	})

	t.Run("RFC 5322 formatted recipients use bare emails in envelope", func(t *testing.T) {
		t.Parallel()

		server, cfg := newTestSMTPServer(t)
		sender := smtp.New(cfg)

		err := sender.Send(context.Background(), &mailer.Email{
			To:      []string{"John Doe <john@example.com>", `"Jane Smith" <jane@example.com>`},
			CC:      []string{"Team <team@example.com>"},
			Subject: "Formatted Recipients",
			Text:    "Recipients have display names",
		})
		require.NoError(t, err)

		msgs := waitForMessages(t, server, 1)

		rcpts := msgs[0].RcpttoRequestResponse()
		require.Len(t, rcpts, 3)

		// SMTP envelope should use bare email addresses
		var addrs []string
		for _, pair := range rcpts {
			addrs = append(addrs, pair[0])
		}
		joined := strings.Join(addrs, " ")
		require.Contains(t, joined, "john@example.com")
		require.Contains(t, joined, "jane@example.com")
		require.Contains(t, joined, "team@example.com")
	})

	t.Run("large email with multiple attachments", func(t *testing.T) {
		t.Parallel()

		server, cfg := newTestSMTPServer(t)
		sender := smtp.New(cfg)

		largeContent := make([]byte, 1024*100) // 100KB
		for i := range largeContent {
			largeContent[i] = byte(i % 256)
		}

		err := sender.Send(context.Background(), &mailer.Email{
			To:      []string{"user@example.com"},
			Subject: "Large Email Test",
			HTML:    "<p>Large attachments</p>",
			Attachments: []mailer.Attachment{
				{
					Filename:    "data1.bin",
					ContentType: "application/octet-stream",
					Content:     largeContent,
				},
				{
					Filename:    "data2.bin",
					ContentType: "application/octet-stream",
					Content:     largeContent,
				},
			},
		})
		require.NoError(t, err)

		msgs := waitForMessages(t, server, 1)
		require.True(t, msgs[0].IsConsistent())
	})
}

func TestNew(t *testing.T) {
	t.Parallel()

	t.Run("creates sender with valid config", func(t *testing.T) {
		t.Parallel()

		sender := smtp.New(smtp.Config{
			Host:        "smtp.example.com",
			Port:        587,
			Username:    "user",
			Password:    "pass",
			SenderEmail: "sender@example.com",
			SenderName:  "Sender Name",
			TLS:         true,
		})
		require.NotNil(t, sender)
	})

	t.Run("creates sender with minimal config", func(t *testing.T) {
		t.Parallel()

		sender := smtp.New(smtp.Config{
			Host: "localhost",
			Port: 1025,
		})
		require.NotNil(t, sender)
	})
}
