package email

import (
	"fmt"
	"net/mail"
	"net/textproto"
	"strings"
)

// Message is one outbound email. From and every recipient are RFC 5322
// addresses — bare ("a@example.com") or named ("Ann <a@example.com>"). At
// least one recipient (To, Cc, or Bcc) and at least one body (Text or HTML)
// are required. Bcc recipients ride the SMTP envelope only and never appear
// in the encoded headers.
type Message struct {
	Headers     map[string]string // extra top-level headers; encoder-owned names are rejected
	From        string
	ReplyTo     string
	Subject     string
	Text        string
	HTML        string
	To          []string
	Cc          []string
	Bcc         []string
	Attachments []Attachment
}

// Attachment is one file carried by a Message. ContentType is optional — an
// empty value is inferred from the filename extension, falling back to
// application/octet-stream. Inline attachments are embedded for HTML display
// (images referenced as <img src="cid:FILENAME">) instead of being listed
// for download; the Content-ID is the filename.
type Attachment struct {
	Filename    string
	ContentType string
	Content     []byte
	Inline      bool
}

// reservedHeaders are written by the encoder itself; a custom header with one
// of these names would duplicate or contradict them (and a consumer-set Bcc
// header would leak hidden recipients), so validation rejects the collision.
var reservedHeaders = map[string]struct{}{
	"From": {}, "To": {}, "Cc": {}, "Bcc": {}, "Reply-To": {}, "Subject": {},
	"Date": {}, "Mime-Version": {}, "Content-Type": {}, "Content-Transfer-Encoding": {},
}

// envelope is the validated, parsed form of a Message: display headers keep
// their names, rcpts is the deduplicated union of To+Cc+Bcc addr-specs for
// the SMTP envelope.
type envelope struct {
	from    *mail.Address
	replyTo *mail.Address
	to      []*mail.Address
	cc      []*mail.Address
	rcpts   []string
}

// validate parses every address and checks the message contract, returning
// the envelope the encoder and sender share. All failures wrap
// ErrInvalidMessage.
func (m *Message) validate() (envelope, error) {
	var env envelope
	var err error
	if env.from, err = parseAddr("From", m.From); err != nil {
		return envelope{}, err
	}
	if m.ReplyTo != "" {
		if env.replyTo, err = parseAddr("Reply-To", m.ReplyTo); err != nil {
			return envelope{}, err
		}
	}
	seen := make(map[string]struct{}, len(m.To)+len(m.Cc)+len(m.Bcc))
	env.rcpts = make([]string, 0, len(m.To)+len(m.Cc)+len(m.Bcc))
	collect := func(field string, addrs []string) ([]*mail.Address, error) {
		parsed := make([]*mail.Address, 0, len(addrs))
		for _, raw := range addrs {
			a, err := parseAddr(field, raw)
			if err != nil {
				return nil, err
			}
			parsed = append(parsed, a)
			if _, dup := seen[a.Address]; !dup {
				seen[a.Address] = struct{}{}
				env.rcpts = append(env.rcpts, a.Address)
			}
		}
		return parsed, nil
	}
	if env.to, err = collect("To", m.To); err != nil {
		return envelope{}, err
	}
	if env.cc, err = collect("Cc", m.Cc); err != nil {
		return envelope{}, err
	}
	if _, err = collect("Bcc", m.Bcc); err != nil {
		return envelope{}, err
	}
	if len(env.rcpts) == 0 {
		return envelope{}, fmt.Errorf("%w: no recipients", ErrInvalidMessage)
	}
	if m.Text == "" && m.HTML == "" {
		return envelope{}, fmt.Errorf("%w: no body", ErrInvalidMessage)
	}
	if strings.ContainsAny(m.Subject, "\r\n") {
		return envelope{}, fmt.Errorf("%w: newline in subject", ErrInvalidMessage)
	}
	for k, v := range m.Headers {
		if strings.ContainsAny(k, "\r\n") || strings.ContainsAny(v, "\r\n") {
			return envelope{}, fmt.Errorf("%w: newline in header %q", ErrInvalidMessage, k)
		}
		if k == "" {
			return envelope{}, fmt.Errorf("%w: empty header name", ErrInvalidMessage)
		}
		if _, reserved := reservedHeaders[textproto.CanonicalMIMEHeaderKey(k)]; reserved {
			return envelope{}, fmt.Errorf("%w: header %q is set by the encoder", ErrInvalidMessage, k)
		}
	}
	for _, a := range m.Attachments {
		if a.Filename == "" {
			return envelope{}, fmt.Errorf("%w: attachment without filename", ErrInvalidMessage)
		}
		if strings.ContainsAny(a.Filename, "\r\n\"") {
			return envelope{}, fmt.Errorf("%w: invalid attachment filename %q", ErrInvalidMessage, a.Filename)
		}
	}
	return env, nil
}

// parseAddr parses one RFC 5322 address, naming the field in the error.
// mail.ParseAddress rejects bare CR/LF, so parsed addresses are
// injection-safe by construction.
func parseAddr(field, raw string) (*mail.Address, error) {
	a, err := mail.ParseAddress(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %s address %q: %v", ErrInvalidMessage, field, raw, err)
	}
	return a, nil
}
