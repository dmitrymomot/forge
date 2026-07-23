// Package email sends transactional email: a Message type with a full RFC
// 5322 / MIME encoder (multipart alternative/related/mixed, quoted-printable
// bodies, base64 attachments, inline images), a Sender seam, a stdlib
// net/smtp implementation with mandatory-by-default STARTTLS, and
// named-template rendering of subject + HTML + text into a Message.
//
// Validation is fail-closed: unparseable addresses, CR/LF in any header
// value (injection), encoder-owned header collisions, and messages with no
// recipient or no body are construction errors. Bcc recipients ride the SMTP
// envelope only and are never encoded. SMTP rejections map onto a queue's
// retry decision — ErrTransient for 4xx, ErrPermanent for 5xx — and a
// failure after the server accepted the message never surfaces as an error,
// so an at-least-once queue cannot double-send.
//
// The comms/email/markdown subpackage renders markdown with YAML frontmatter
// into a ready Message — the designer-free transactional format.
//
// # Non-goals
//
//   - No provider SDKs: SES/Postmark/Mailgun adapters are consumer-side
//     Sender implementations over Message.Encode or their JSON APIs.
//   - No durable delivery, retries, or rate limiting: ride async/queue with
//     a Sender as the deliverer; the error sentinels carry the retry
//     decision.
//   - No inbound processing: parsing received mail is comms/inbound.
//   - No connection pooling: submission servers are dial-per-message
//     friendly; a pool would trade correctness (stale-connection races) for
//     an unproven win.
//   - No tenant seam: a Message is a passed value and the sender holds no
//     tenant data; per-tenant identities are per-tenant Config values.
//
// # Usage
//
//	sender, err := email.New(email.Config{
//		Addr:     "smtp.example.com:587",
//		Username: "postmaster",
//		Password: "secret",
//		TLS:      email.TLSStartTLS,
//		From:     "Acme <no-reply@acme.example>",
//		Timeout:  15 * time.Second,
//	})
//	if err != nil {
//		// bad config
//	}
//
//	tpl, _ := email.ParseFS(templatesFS, "templates/*.tmpl")
//	msg, err := tpl.Render("welcome", map[string]string{"Name": "Ann"})
//	if err != nil {
//		// unknown name or broken template
//	}
//	msg.To = []string{"Ann <ann@example.com>"}
//
//	err = sender.Send(ctx, msg)
//	// errors.Is(err, email.ErrTransient) → requeue
//	// errors.Is(err, email.ErrPermanent) → dead-letter
package email
