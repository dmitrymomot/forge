package smtp

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"strings"
	"time"

	"github.com/dmitrymomot/forge/pkg/id"
	"github.com/dmitrymomot/forge/pkg/mailer"
)

// buildMessage constructs a complete MIME email message from the given sender
// address and email struct. It returns the raw message bytes ready for SMTP DATA.
func buildMessage(from string, email *mailer.Email) ([]byte, error) {
	var buf bytes.Buffer

	writeHeader(&buf, "From", from)
	writeHeader(&buf, "To", strings.Join(email.To, ", "))
	if len(email.CC) > 0 {
		writeHeader(&buf, "Cc", strings.Join(email.CC, ", "))
	}
	if email.ReplyTo != "" {
		writeHeader(&buf, "Reply-To", email.ReplyTo)
	}
	writeHeader(&buf, "Subject", mime.QEncoding.Encode("utf-8", email.Subject))
	writeHeader(&buf, "Date", time.Now().Format(time.RFC1123Z))
	writeHeader(&buf, "Message-ID", messageID(from))
	writeHeader(&buf, "MIME-Version", "1.0")

	for k, v := range email.Headers {
		writeHeader(&buf, k, v)
	}

	hasHTML := email.HTML != ""
	hasText := email.Text != ""
	hasAttachments := len(email.Attachments) > 0

	switch {
	case hasAttachments:
		if err := writeMixedBody(&buf, email, hasHTML, hasText); err != nil {
			return nil, err
		}
	case hasHTML && hasText:
		if err := writeAlternativeBody(&buf, email.Text, email.HTML); err != nil {
			return nil, err
		}
	case hasHTML:
		if err := writeSingleBody(&buf, "text/html", email.HTML); err != nil {
			return nil, err
		}
	case hasText:
		if err := writeSingleBody(&buf, "text/plain", email.Text); err != nil {
			return nil, err
		}
	}

	return buf.Bytes(), nil
}

// writeSingleBody writes a single-type body (text/plain or text/html) directly
// to the buffer with quoted-printable encoding.
func writeSingleBody(buf *bytes.Buffer, contentType, body string) error {
	writeHeader(buf, "Content-Type", contentType+"; charset=utf-8")
	writeHeader(buf, "Content-Transfer-Encoding", "quoted-printable")
	buf.WriteString("\r\n")
	w := quotedprintable.NewWriter(buf)
	if _, err := w.Write([]byte(body)); err != nil {
		return fmt.Errorf("smtp: encode body: %w", err)
	}
	return w.Close()
}

// writeMixedBody writes a multipart/mixed message containing the body
// (as multipart/alternative when both HTML and text are present) plus attachments.
func writeMixedBody(buf *bytes.Buffer, email *mailer.Email, hasHTML, hasText bool) error {
	mixedWriter := multipart.NewWriter(buf)
	writeHeader(buf, "Content-Type", "multipart/mixed; boundary="+mixedWriter.Boundary())
	buf.WriteString("\r\n")

	switch {
	case hasHTML && hasText:
		if err := writeAlternativePart(mixedWriter, email.Text, email.HTML); err != nil {
			return err
		}
	case hasHTML:
		if err := writeQPPart(mixedWriter, "text/html", email.HTML); err != nil {
			return err
		}
	case hasText:
		if err := writeQPPart(mixedWriter, "text/plain", email.Text); err != nil {
			return err
		}
	}

	for i := range email.Attachments {
		if err := writeAttachment(mixedWriter, &email.Attachments[i]); err != nil {
			return err
		}
	}

	return mixedWriter.Close()
}

// writeAlternativeBody writes a multipart/alternative body with text/plain
// and text/html parts directly to the buffer (used when there are no attachments).
func writeAlternativeBody(buf *bytes.Buffer, text, html string) error {
	altWriter := multipart.NewWriter(buf)
	writeHeader(buf, "Content-Type", "multipart/alternative; boundary="+altWriter.Boundary())
	buf.WriteString("\r\n")

	if err := writeQPPart(altWriter, "text/plain", text); err != nil {
		return err
	}
	if err := writeQPPart(altWriter, "text/html", html); err != nil {
		return err
	}

	return altWriter.Close()
}

// writeAlternativePart writes a multipart/alternative body as a nested part
// inside a parent multipart writer (used when attachments are present).
func writeAlternativePart(parent *multipart.Writer, text, html string) error {
	boundary := multipart.NewWriter(io.Discard).Boundary()

	h := make(textproto.MIMEHeader)
	h.Set("Content-Type", "multipart/alternative; boundary="+boundary)

	part, err := parent.CreatePart(h)
	if err != nil {
		return fmt.Errorf("smtp: create alternative part: %w", err)
	}

	altWriter := multipart.NewWriter(part)
	if err := altWriter.SetBoundary(boundary); err != nil {
		return fmt.Errorf("smtp: set alternative boundary: %w", err)
	}

	if err := writeQPPart(altWriter, "text/plain", text); err != nil {
		return err
	}
	if err := writeQPPart(altWriter, "text/html", html); err != nil {
		return err
	}

	return altWriter.Close()
}

// writeQPPart writes a quoted-printable encoded part into the given multipart writer.
func writeQPPart(w *multipart.Writer, contentType, body string) error {
	h := make(textproto.MIMEHeader)
	h.Set("Content-Type", contentType+"; charset=utf-8")
	h.Set("Content-Transfer-Encoding", "quoted-printable")

	part, err := w.CreatePart(h)
	if err != nil {
		return fmt.Errorf("smtp: create %s part: %w", contentType, err)
	}

	qw := quotedprintable.NewWriter(part)
	if _, err := qw.Write([]byte(body)); err != nil {
		return fmt.Errorf("smtp: encode %s part: %w", contentType, err)
	}
	return qw.Close()
}

// writeAttachment writes a base64-encoded attachment part with appropriate
// Content-Disposition and optional Content-ID for inline attachments.
func writeAttachment(w *multipart.Writer, att *mailer.Attachment) error {
	h := make(textproto.MIMEHeader)

	ct := att.ContentType
	if ct == "" {
		ct = "application/octet-stream"
	}
	h.Set("Content-Type", ct+"; name=\""+att.Filename+"\"")
	h.Set("Content-Transfer-Encoding", "base64")

	if att.ContentID != "" {
		// Strip any pre-existing angle brackets so we don't produce a
		// malformed "<<cid>>" Content-ID header.
		cid := strings.Trim(att.ContentID, "<>")
		h.Set("Content-Disposition", "inline; filename=\""+att.Filename+"\"")
		h.Set("Content-ID", "<"+cid+">")
	} else {
		h.Set("Content-Disposition", "attachment; filename=\""+att.Filename+"\"")
	}

	part, err := w.CreatePart(h)
	if err != nil {
		return fmt.Errorf("smtp: create attachment part: %w", err)
	}

	encoder := base64.NewEncoder(base64.StdEncoding, part)
	if _, err := encoder.Write(att.Content); err != nil {
		return fmt.Errorf("smtp: encode attachment: %w", err)
	}
	return encoder.Close()
}

// messageID builds an RFC 5322 Message-ID of the form "<unique@domain>".
// The unique part comes from pkg/id (the project's mandated ID generator) and
// the domain is derived from the sender's address, falling back to "localhost"
// when it cannot be parsed.
func messageID(from string) string {
	domain := "localhost"
	if addr, err := mail.ParseAddress(from); err == nil {
		if at := strings.LastIndex(addr.Address, "@"); at >= 0 && at < len(addr.Address)-1 {
			domain = addr.Address[at+1:]
		}
	}
	return "<" + id.NewULID() + "@" + domain + ">"
}

// writeHeader writes a single MIME header line to the buffer.
func writeHeader(buf *bytes.Buffer, key, value string) {
	buf.WriteString(key)
	buf.WriteString(": ")
	buf.WriteString(value)
	buf.WriteString("\r\n")
}
