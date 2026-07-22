package email

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"maps"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"path"
	"slices"
	"strings"
	"time"
)

// Encode writes the message as a complete RFC 5322 document with CRLF line
// endings: headers, then a MIME tree of quoted-printable bodies and base64
// attachments (multipart/alternative under multipart/related under
// multipart/mixed, each level present only when needed). Bcc recipients are
// never written. The output is what provider raw-send APIs (SES, Postmark)
// accept; Send streams the same bytes over SMTP.
func (m *Message) Encode(w io.Writer) error {
	env, err := m.validate()
	if err != nil {
		return err
	}
	return m.encode(w, env, time.Now())
}

func (m *Message) encode(w io.Writer, env envelope, now time.Time) error {
	bw := bufio.NewWriter(w)
	writeHeader(bw, "Date", now.Format(time.RFC1123Z))
	if !m.hasCustomHeader("Message-Id") {
		writeHeader(bw, "Message-ID", messageID(env.from))
	}
	writeHeader(bw, "From", env.from.String())
	if len(env.to) > 0 {
		writeHeader(bw, "To", joinAddrs(env.to))
	}
	if len(env.cc) > 0 {
		writeHeader(bw, "Cc", joinAddrs(env.cc))
	}
	if env.replyTo != nil {
		writeHeader(bw, "Reply-To", env.replyTo.String())
	}
	if m.Subject != "" {
		writeHeader(bw, "Subject", mime.QEncoding.Encode("utf-8", m.Subject))
	}
	for _, k := range slices.Sorted(maps.Keys(m.Headers)) {
		writeHeader(bw, k, m.Headers[k])
	}
	writeHeader(bw, "MIME-Version", "1.0")

	body := m.body()
	for _, k := range slices.Sorted(maps.Keys(body.header)) {
		writeHeader(bw, k, body.header.Get(k))
	}
	bw.WriteString("\r\n") //nolint:errcheck // surfaced by Flush
	if err := body.write(bw); err != nil {
		return err
	}
	if err := bw.Flush(); err != nil {
		return fmt.Errorf("email: encode: %w", err)
	}
	return nil
}

func (m *Message) hasCustomHeader(canonical string) bool {
	for k := range m.Headers {
		if textproto.CanonicalMIMEHeaderKey(k) == canonical {
			return true
		}
	}
	return false
}

func writeHeader(bw *bufio.Writer, name, value string) {
	bw.WriteString(name)   //nolint:errcheck // surfaced by Flush
	bw.WriteString(": ")   //nolint:errcheck // surfaced by Flush
	bw.WriteString(value)  //nolint:errcheck // surfaced by Flush
	bw.WriteString("\r\n") //nolint:errcheck // surfaced by Flush
}

func joinAddrs(addrs []*mail.Address) string {
	var sb strings.Builder
	for i, a := range addrs {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(a.String())
	}
	return sb.String()
}

// messageID builds a globally unique Message-ID under the sender's domain so
// replies thread correctly; consumers needing a stable ID set Message-Id via
// Headers instead.
func messageID(from *mail.Address) string {
	var buf [16]byte
	rand.Read(buf[:]) //nolint:errcheck // crypto/rand.Read never fails
	domain := from.Address
	if at := strings.LastIndexByte(domain, '@'); at >= 0 {
		domain = domain[at+1:]
	}
	return "<" + hex.EncodeToString(buf[:]) + "@" + domain + ">"
}

// part is one node of the MIME tree: its headers plus a body writer. Leaves
// encode a text body or attachment; interior nodes are multipart containers.
type part struct {
	header textproto.MIMEHeader
	write  func(io.Writer) error
}

// body assembles the MIME tree for the message's content. Validation has
// already guaranteed at least one of Text/HTML is present.
func (m *Message) body() part {
	var inline, files []Attachment
	for _, a := range m.Attachments {
		if a.Inline {
			inline = append(inline, a)
		} else {
			files = append(files, a)
		}
	}

	parts := make([]part, 0, 2)
	if m.Text != "" {
		parts = append(parts, textPart("text/plain", m.Text))
	}
	if m.HTML != "" {
		parts = append(parts, textPart("text/html", m.HTML))
	}
	core := parts[0]
	if len(parts) == 2 {
		core = containerPart("alternative", parts)
	}
	if len(inline) > 0 {
		related := make([]part, 0, 1+len(inline))
		related = append(related, core)
		for _, a := range inline {
			related = append(related, attachmentPart(a))
		}
		core = containerPart("related", related)
	}
	if len(files) > 0 {
		mixed := make([]part, 0, 1+len(files))
		mixed = append(mixed, core)
		for _, a := range files {
			mixed = append(mixed, attachmentPart(a))
		}
		core = containerPart("mixed", mixed)
	}
	return core
}

func textPart(contentType, content string) part {
	return part{
		header: textproto.MIMEHeader{
			"Content-Type":              {contentType + "; charset=utf-8"},
			"Content-Transfer-Encoding": {"quoted-printable"},
		},
		write: func(w io.Writer) error {
			qp := quotedprintable.NewWriter(w)
			if _, err := io.WriteString(qp, content); err != nil {
				return fmt.Errorf("email: encode body: %w", err)
			}
			if err := qp.Close(); err != nil {
				return fmt.Errorf("email: encode body: %w", err)
			}
			return nil
		},
	}
}

func attachmentPart(a Attachment) part {
	ct := a.ContentType
	if ct == "" {
		ct = mime.TypeByExtension(strings.ToLower(path.Ext(a.Filename)))
		if ct == "" {
			ct = "application/octet-stream"
		}
	}
	filename := mime.QEncoding.Encode("utf-8", a.Filename)
	h := textproto.MIMEHeader{
		"Content-Type":              {ct + `; name="` + filename + `"`},
		"Content-Transfer-Encoding": {"base64"},
	}
	if a.Inline {
		h.Set("Content-Disposition", `inline; filename="`+filename+`"`)
		h.Set("Content-Id", "<"+a.Filename+">")
	} else {
		h.Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	}
	return part{
		header: h,
		write: func(w io.Writer) error {
			lw := &lineWrapper{w: w}
			enc := base64.NewEncoder(base64.StdEncoding, lw)
			if _, err := enc.Write(a.Content); err != nil {
				return fmt.Errorf("email: encode attachment %q: %w", a.Filename, err)
			}
			if err := enc.Close(); err != nil {
				return fmt.Errorf("email: encode attachment %q: %w", a.Filename, err)
			}
			if lw.col > 0 {
				if _, err := w.Write(crlf); err != nil {
					return fmt.Errorf("email: encode attachment %q: %w", a.Filename, err)
				}
			}
			return nil
		},
	}
}

func containerPart(subtype string, children []part) part {
	boundary := randomBoundary()
	return part{
		header: textproto.MIMEHeader{
			"Content-Type": {"multipart/" + subtype + `; boundary="` + boundary + `"`},
		},
		write: func(w io.Writer) error {
			mw := multipart.NewWriter(w)
			if err := mw.SetBoundary(boundary); err != nil {
				return fmt.Errorf("email: encode multipart: %w", err)
			}
			for _, child := range children {
				pw, err := mw.CreatePart(child.header)
				if err != nil {
					return fmt.Errorf("email: encode multipart: %w", err)
				}
				if err := child.write(pw); err != nil {
					return err
				}
			}
			if err := mw.Close(); err != nil {
				return fmt.Errorf("email: encode multipart: %w", err)
			}
			return nil
		},
	}
}

func randomBoundary() string {
	var buf [18]byte
	rand.Read(buf[:]) //nolint:errcheck // crypto/rand.Read never fails
	return "part-" + hex.EncodeToString(buf[:])
}

// crlf is a shared byte form of the line terminator: io.WriteString would
// re-allocate it per write, and the base64 folder writes it once per line.
var crlf = []byte("\r\n")

// lineWrapper folds a base64 stream at 76 columns with CRLF, per RFC 2045.
type lineWrapper struct {
	w   io.Writer
	col int
}

func (lw *lineWrapper) Write(p []byte) (int, error) {
	total := len(p)
	for len(p) > 0 {
		if lw.col == 76 {
			if _, err := lw.w.Write(crlf); err != nil {
				return total - len(p), err
			}
			lw.col = 0
		}
		chunk := min(76-lw.col, len(p))
		if _, err := lw.w.Write(p[:chunk]); err != nil {
			return total - len(p), err
		}
		lw.col += chunk
		p = p[chunk:]
	}
	return total, nil
}
