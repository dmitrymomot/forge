package email_test

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/comms/email"
)

// parsedPart is one leaf of the decoded MIME tree, keyed for assertions.
type parsedPart struct {
	contentType string
	disposition string
	contentID   string
	body        []byte
}

// flattenParts walks a body, recursing into multipart containers and
// decoding leaf transfer encodings.
func flattenParts(t *testing.T, contentType string, encoding string, body io.Reader) []parsedPart {
	t.Helper()
	mediaType, params, err := mime.ParseMediaType(contentType)
	require.NoError(t, err)
	if !strings.HasPrefix(mediaType, "multipart/") {
		var decoded []byte
		switch encoding {
		case "quoted-printable":
			decoded, err = io.ReadAll(quotedprintable.NewReader(body))
		case "base64":
			decoded, err = io.ReadAll(base64.NewDecoder(base64.StdEncoding, body))
		default:
			decoded, err = io.ReadAll(body)
		}
		require.NoError(t, err)
		return []parsedPart{{contentType: mediaType, body: decoded}}
	}
	mr := multipart.NewReader(body, params["boundary"])
	parts := make([]parsedPart, 0, 4)
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			return parts
		}
		require.NoError(t, err)
		sub := flattenParts(t, p.Header.Get("Content-Type"), p.Header.Get("Content-Transfer-Encoding"), p)
		for i := range sub {
			if sub[i].disposition == "" {
				sub[i].disposition = p.Header.Get("Content-Disposition")
			}
			if sub[i].contentID == "" {
				sub[i].contentID = p.Header.Get("Content-Id")
			}
		}
		parts = append(parts, sub...)
	}
}

func encodeAndParse(t *testing.T, msg email.Message) (*mail.Message, []parsedPart) {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, msg.Encode(&buf))
	parsed, err := mail.ReadMessage(bytes.NewReader(buf.Bytes()))
	require.NoError(t, err)
	parts := flattenParts(t, parsed.Header.Get("Content-Type"), parsed.Header.Get("Content-Transfer-Encoding"), parsed.Body)
	return parsed, parts
}

func TestEncodeTextOnly(t *testing.T) {
	t.Parallel()

	msg := validMessage()
	msg.Text = "Привіт, Ann!\nLine two."
	parsed, parts := encodeAndParse(t, msg)

	require.Len(t, parts, 1)
	assert.Equal(t, "text/plain", parts[0].contentType)
	assert.Equal(t, "Привіт, Ann!\r\nLine two.", string(parts[0].body))
	assert.Equal(t, "1.0", parsed.Header.Get("MIME-Version"))

	_, err := parsed.Header.Date()
	assert.NoError(t, err, "Date header must parse")
	from, err := mail.ParseAddress(parsed.Header.Get("From"))
	require.NoError(t, err)
	assert.Equal(t, "no-reply@acme.example", from.Address)
	assert.Equal(t, "Acme", from.Name)
	assert.Regexp(t, regexp.MustCompile(`^<[0-9a-f]{32}@acme\.example>$`), parsed.Header.Get("Message-Id"))
}

func TestEncodeAlternative(t *testing.T) {
	t.Parallel()

	msg := validMessage()
	msg.HTML = "<p>Hi <b>Ann</b>,</p>"
	parsed, parts := encodeAndParse(t, msg)

	require.Len(t, parts, 2)
	assert.Equal(t, "text/plain", parts[0].contentType, "text must precede html so clients prefer html (last part wins)")
	assert.Equal(t, "text/html", parts[1].contentType)
	assert.Equal(t, "<p>Hi <b>Ann</b>,</p>", string(parts[1].body))
	mediaType, _, err := mime.ParseMediaType(parsed.Header.Get("Content-Type"))
	require.NoError(t, err)
	assert.Equal(t, "multipart/alternative", mediaType)
}

func TestEncodeAttachments(t *testing.T) {
	t.Parallel()

	payload := bytes.Repeat([]byte{0x00, 0xff, 0x10, 0x42}, 300) // forces base64 line folding
	msg := validMessage()
	msg.HTML = `<p>See <img src="cid:logo.png"> attached.</p>`
	msg.Attachments = []email.Attachment{
		{Filename: "report.pdf", Content: payload},
		{Filename: "logo.png", Content: []byte("PNGDATA"), Inline: true},
	}
	parsed, parts := encodeAndParse(t, msg)

	mediaType, _, err := mime.ParseMediaType(parsed.Header.Get("Content-Type"))
	require.NoError(t, err)
	assert.Equal(t, "multipart/mixed", mediaType)

	require.Len(t, parts, 4) // text, html, inline logo, attached report
	byType := map[string]parsedPart{}
	for _, p := range parts {
		byType[p.contentType] = p
	}
	pdf, ok := byType["application/pdf"]
	require.True(t, ok, "content type inferred from .pdf extension, got %v", parts)
	assert.Equal(t, payload, pdf.body)
	assert.Contains(t, pdf.disposition, "attachment")
	assert.Contains(t, pdf.disposition, `filename="report.pdf"`)

	png, ok := byType["image/png"]
	require.True(t, ok, "content type inferred from .png extension")
	assert.Equal(t, "PNGDATA", string(png.body))
	assert.Contains(t, png.disposition, "inline")
	assert.Equal(t, "<logo.png>", png.contentID)
}

func TestEncodeUnknownAttachmentType(t *testing.T) {
	t.Parallel()

	msg := validMessage()
	msg.Attachments = []email.Attachment{
		{Filename: "blob.weirdext123", Content: []byte("x")},
		{Filename: "noext", Content: []byte("y")},
		{Filename: "typed.bin", ContentType: "application/x-custom", Content: []byte("z")},
	}
	_, parts := encodeAndParse(t, msg)
	var types []string
	for _, p := range parts {
		if p.contentType == "text/plain" {
			continue
		}
		types = append(types, p.contentType)
	}
	assert.ElementsMatch(t, []string{"application/octet-stream", "application/octet-stream", "application/x-custom"}, types)
}

func TestEncodeHeaders(t *testing.T) {
	t.Parallel()

	msg := email.Message{
		From:    "no-reply@acme.example",
		To:      []string{"Ann <ann@example.com>", "bob@example.com"},
		Cc:      []string{"carol@example.com"},
		Bcc:     []string{"hidden@example.com"},
		ReplyTo: "Support <support@acme.example>",
		Subject: "Звіт за липень ✨",
		Text:    "body",
		Headers: map[string]string{"X-Campaign": "july", "Message-Id": "<stable-42@acme.example>"},
	}
	var buf bytes.Buffer
	require.NoError(t, msg.Encode(&buf))
	raw := buf.String()
	assert.NotContains(t, raw, "hidden@example.com", "Bcc must never be encoded")
	assert.NotContains(t, raw, "Звіт", "non-ASCII subject must be word-encoded")

	parsed, err := mail.ReadMessage(bytes.NewReader(buf.Bytes()))
	require.NoError(t, err)
	subject, err := new(mime.WordDecoder).DecodeHeader(parsed.Header.Get("Subject"))
	require.NoError(t, err)
	assert.Equal(t, "Звіт за липень ✨", subject)

	to, err := parsed.Header.AddressList("To")
	require.NoError(t, err)
	var toAddrs []string
	for _, a := range to {
		toAddrs = append(toAddrs, a.String())
	}
	assert.Equal(t, []string{`"Ann" <ann@example.com>`, "<bob@example.com>"}, toAddrs)
	replyTo, err := mail.ParseAddress(parsed.Header.Get("Reply-To"))
	require.NoError(t, err)
	assert.Equal(t, "support@acme.example", replyTo.Address)
	assert.Equal(t, "<stable-42@acme.example>", parsed.Header.Get("Message-Id"), "consumer Message-Id wins over generation")
	assert.Equal(t, "july", parsed.Header.Get("X-Campaign"))
}

func TestEncodeQuotedPrintableEdge(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("wide характер text ", 40) // forces soft line breaks
	msg := validMessage()
	msg.Text = long + "\ntrailing=equals=signs"
	_, parts := encodeAndParse(t, msg)
	require.Len(t, parts, 1)
	assert.Equal(t, long+"\r\ntrailing=equals=signs", string(parts[0].body))
}

func TestEncodeDeterministicHeaderOrder(t *testing.T) {
	t.Parallel()

	msg := validMessage()
	msg.Headers = map[string]string{"X-B": "2", "X-A": "1", "X-C": "3", "Message-Id": "<fixed@acme.example>"}
	var first bytes.Buffer
	require.NoError(t, msg.Encode(&first))
	idxA := strings.Index(first.String(), "X-A: 1")
	idxB := strings.Index(first.String(), "X-B: 2")
	idxC := strings.Index(first.String(), "X-C: 3")
	require.True(t, idxA >= 0 && idxB >= 0 && idxC >= 0)
	assert.True(t, idxA < idxB && idxB < idxC, "custom headers must encode in sorted order")
}

func TestEncodeDateIsNow(t *testing.T) {
	t.Parallel()

	before := time.Now().Add(-time.Minute)
	parsed, _ := encodeAndParse(t, validMessage())
	date, err := parsed.Header.Date()
	require.NoError(t, err)
	assert.WithinRange(t, date, before, time.Now().Add(time.Minute))
}
