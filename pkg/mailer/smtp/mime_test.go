package smtp

import (
	"bytes"
	"mime/multipart"
	"net/mail"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/pkg/mailer"
)

func TestBuildMessage(t *testing.T) {
	t.Parallel()

	t.Run("HTML only message", func(t *testing.T) {
		t.Parallel()

		email := &mailer.Email{
			Subject: "Test Subject",
			HTML:    "<html><body>Hello World</body></html>",
			To:      []string{"recipient@example.com"},
		}

		msg, err := buildMessage("sender@example.com", email)
		require.NoError(t, err)

		msgStr := string(msg)
		require.Contains(t, msgStr, "From: sender@example.com\r\n")
		require.Contains(t, msgStr, "To: recipient@example.com\r\n")
		require.Contains(t, msgStr, "Subject: Test Subject\r\n")
		require.Contains(t, msgStr, "MIME-Version: 1.0\r\n")
		require.Contains(t, msgStr, "Content-Type: text/html; charset=utf-8\r\n")
		require.Contains(t, msgStr, "Content-Transfer-Encoding: quoted-printable\r\n")
		require.Contains(t, msgStr, "<html><body>Hello World</body></html>")
		require.NotContains(t, msgStr, "multipart")
	})

	t.Run("text only message", func(t *testing.T) {
		t.Parallel()

		email := &mailer.Email{
			Subject: "Plain Text",
			Text:    "This is plain text content.",
			To:      []string{"user@example.com"},
		}

		msg, err := buildMessage("from@example.com", email)
		require.NoError(t, err)

		msgStr := string(msg)
		require.Contains(t, msgStr, "Content-Type: text/plain; charset=utf-8\r\n")
		require.Contains(t, msgStr, "Content-Transfer-Encoding: quoted-printable\r\n")
		require.Contains(t, msgStr, "This is plain text content.")
		require.NotContains(t, msgStr, "multipart")
	})

	t.Run("HTML and text (multipart/alternative)", func(t *testing.T) {
		t.Parallel()

		email := &mailer.Email{
			Subject: "Both Formats",
			HTML:    "<html><body><p>HTML version</p></body></html>",
			Text:    "Plain text version",
			To:      []string{"user@example.com"},
		}

		msg, err := buildMessage("sender@example.com", email)
		require.NoError(t, err)

		msgStr := string(msg)
		require.Contains(t, msgStr, "Content-Type: multipart/alternative; boundary=")
		require.Contains(t, msgStr, "Content-Type: text/plain; charset=utf-8\r\n")
		require.Contains(t, msgStr, "Content-Type: text/html; charset=utf-8\r\n")
		require.Contains(t, msgStr, "Plain text version")
		require.Contains(t, msgStr, "HTML version")
		require.NotContains(t, msgStr, "multipart/mixed")
	})

	t.Run("HTML with attachment (multipart/mixed)", func(t *testing.T) {
		t.Parallel()

		email := &mailer.Email{
			Subject: "With Attachment",
			HTML:    "<p>See attached file</p>",
			To:      []string{"user@example.com"},
			Attachments: []mailer.Attachment{
				{
					Filename:    "document.pdf",
					ContentType: "application/pdf",
					Content:     []byte("fake pdf content"),
				},
			},
		}

		msg, err := buildMessage("sender@example.com", email)
		require.NoError(t, err)

		msgStr := string(msg)
		require.Contains(t, msgStr, "Content-Type: multipart/mixed; boundary=")
		require.Contains(t, msgStr, "Content-Type: text/html; charset=utf-8\r\n")
		require.Contains(t, msgStr, "Content-Type: application/pdf; name=\"document.pdf\"\r\n")
		require.Contains(t, msgStr, "Content-Disposition: attachment; filename=\"document.pdf\"\r\n")
		require.Contains(t, msgStr, "Content-Transfer-Encoding: base64\r\n")
		require.Contains(t, msgStr, "See attached file")
	})

	t.Run("HTML and text with attachments (nested multipart)", func(t *testing.T) {
		t.Parallel()

		email := &mailer.Email{
			Subject: "Complex Message",
			HTML:    "<p>HTML body</p>",
			Text:    "Text body",
			To:      []string{"user@example.com"},
			Attachments: []mailer.Attachment{
				{
					Filename:    "file.txt",
					ContentType: "text/plain",
					Content:     []byte("attachment content"),
				},
			},
		}

		msg, err := buildMessage("sender@example.com", email)
		require.NoError(t, err)

		msgStr := string(msg)
		// Outer multipart/mixed
		require.Contains(t, msgStr, "Content-Type: multipart/mixed; boundary=")
		// Nested multipart/alternative for body
		require.Contains(t, msgStr, "Content-Type: multipart/alternative; boundary=")
		require.Contains(t, msgStr, "Content-Type: text/plain; charset=utf-8\r\n")
		require.Contains(t, msgStr, "Content-Type: text/html; charset=utf-8\r\n")
		// Attachment
		require.Contains(t, msgStr, "Content-Type: text/plain; name=\"file.txt\"\r\n")
		require.Contains(t, msgStr, "Content-Disposition: attachment; filename=\"file.txt\"\r\n")
	})

	t.Run("inline attachment with ContentID", func(t *testing.T) {
		t.Parallel()

		email := &mailer.Email{
			Subject: "Inline Image",
			HTML:    `<img src="cid:logo123">`,
			To:      []string{"user@example.com"},
			Attachments: []mailer.Attachment{
				{
					Filename:    "logo.png",
					ContentType: "image/png",
					ContentID:   "logo123",
					Content:     []byte("fake image data"),
				},
			},
		}

		msg, err := buildMessage("sender@example.com", email)
		require.NoError(t, err)

		msgStr := string(msg)
		require.Contains(t, msgStr, "Content-Type: image/png; name=\"logo.png\"\r\n")
		require.Contains(t, msgStr, "Content-Disposition: inline; filename=\"logo.png\"\r\n")
		// MIME headers are case-insensitive, check for Content-Id variant
		require.Contains(t, msgStr, "Content-Id: <logo123>\r\n")
	})

	t.Run("attachment without ContentType defaults to octet-stream", func(t *testing.T) {
		t.Parallel()

		email := &mailer.Email{
			Subject: "Unknown Type",
			Text:    "See attached",
			To:      []string{"user@example.com"},
			Attachments: []mailer.Attachment{
				{
					Filename: "unknown.bin",
					Content:  []byte("binary data"),
				},
			},
		}

		msg, err := buildMessage("sender@example.com", email)
		require.NoError(t, err)

		msgStr := string(msg)
		require.Contains(t, msgStr, "Content-Type: application/octet-stream; name=\"unknown.bin\"\r\n")
	})

	t.Run("multiple recipients in To field", func(t *testing.T) {
		t.Parallel()

		email := &mailer.Email{
			Subject: "Multiple To",
			Text:    "Hello",
			To:      []string{"user1@example.com", "user2@example.com", "user3@example.com"},
		}

		msg, err := buildMessage("sender@example.com", email)
		require.NoError(t, err)

		msgStr := string(msg)
		require.Contains(t, msgStr, "To: user1@example.com, user2@example.com, user3@example.com\r\n")
	})

	t.Run("CC header when CC recipients present", func(t *testing.T) {
		t.Parallel()

		email := &mailer.Email{
			Subject: "With CC",
			Text:    "Content",
			To:      []string{"primary@example.com"},
			CC:      []string{"cc1@example.com", "cc2@example.com"},
		}

		msg, err := buildMessage("sender@example.com", email)
		require.NoError(t, err)

		msgStr := string(msg)
		require.Contains(t, msgStr, "Cc: cc1@example.com, cc2@example.com\r\n")
	})

	t.Run("no CC header when CC list empty", func(t *testing.T) {
		t.Parallel()

		email := &mailer.Email{
			Subject: "No CC",
			Text:    "Content",
			To:      []string{"user@example.com"},
		}

		msg, err := buildMessage("sender@example.com", email)
		require.NoError(t, err)

		msgStr := string(msg)
		require.NotContains(t, msgStr, "Cc:")
	})

	t.Run("Reply-To header when set", func(t *testing.T) {
		t.Parallel()

		email := &mailer.Email{
			Subject: "With Reply-To",
			Text:    "Please reply",
			To:      []string{"user@example.com"},
			ReplyTo: "replies@example.com",
		}

		msg, err := buildMessage("sender@example.com", email)
		require.NoError(t, err)

		msgStr := string(msg)
		require.Contains(t, msgStr, "Reply-To: replies@example.com\r\n")
	})

	t.Run("no Reply-To header when empty", func(t *testing.T) {
		t.Parallel()

		email := &mailer.Email{
			Subject: "No Reply-To",
			Text:    "Content",
			To:      []string{"user@example.com"},
		}

		msg, err := buildMessage("sender@example.com", email)
		require.NoError(t, err)

		msgStr := string(msg)
		require.NotContains(t, msgStr, "Reply-To:")
	})

	t.Run("custom headers included", func(t *testing.T) {
		t.Parallel()

		email := &mailer.Email{
			Subject: "Custom Headers",
			Text:    "Content",
			To:      []string{"user@example.com"},
			Headers: map[string]string{
				"X-Priority":    "1",
				"X-Custom-Flag": "test-value",
			},
		}

		msg, err := buildMessage("sender@example.com", email)
		require.NoError(t, err)

		msgStr := string(msg)
		require.Contains(t, msgStr, "X-Priority: 1\r\n")
		require.Contains(t, msgStr, "X-Custom-Flag: test-value\r\n")
	})

	t.Run("non-ASCII subject is encoded", func(t *testing.T) {
		t.Parallel()

		email := &mailer.Email{
			Subject: "Тест 中文 🎉",
			Text:    "Content",
			To:      []string{"user@example.com"},
		}

		msg, err := buildMessage("sender@example.com", email)
		require.NoError(t, err)

		msgStr := string(msg)
		// Subject should be Q-encoded
		require.Contains(t, msgStr, "Subject: =?utf-8?q?")
		require.NotContains(t, msgStr, "Subject: Тест")
	})

	t.Run("ASCII subject is not encoded", func(t *testing.T) {
		t.Parallel()

		email := &mailer.Email{
			Subject: "Simple ASCII Subject",
			Text:    "Content",
			To:      []string{"user@example.com"},
		}

		msg, err := buildMessage("sender@example.com", email)
		require.NoError(t, err)

		msgStr := string(msg)
		require.Contains(t, msgStr, "Subject: Simple ASCII Subject\r\n")
	})

	t.Run("sender with name in RFC 5322 format", func(t *testing.T) {
		t.Parallel()

		email := &mailer.Email{
			Subject: "Named Sender",
			Text:    "Content",
			To:      []string{"user@example.com"},
		}

		msg, err := buildMessage("John Doe <john@example.com>", email)
		require.NoError(t, err)

		msgStr := string(msg)
		require.Contains(t, msgStr, "From: John Doe <john@example.com>\r\n")
	})

	t.Run("multiple attachments", func(t *testing.T) {
		t.Parallel()

		email := &mailer.Email{
			Subject: "Multiple Files",
			Text:    "See attachments",
			To:      []string{"user@example.com"},
			Attachments: []mailer.Attachment{
				{
					Filename:    "doc1.pdf",
					ContentType: "application/pdf",
					Content:     []byte("pdf content"),
				},
				{
					Filename:    "doc2.docx",
					ContentType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
					Content:     []byte("docx content"),
				},
				{
					Filename:    "image.jpg",
					ContentType: "image/jpeg",
					Content:     []byte("jpeg data"),
				},
			},
		}

		msg, err := buildMessage("sender@example.com", email)
		require.NoError(t, err)

		msgStr := string(msg)
		require.Contains(t, msgStr, "name=\"doc1.pdf\"")
		require.Contains(t, msgStr, "name=\"doc2.docx\"")
		require.Contains(t, msgStr, "name=\"image.jpg\"")
		require.Contains(t, msgStr, "application/pdf")
		require.Contains(t, msgStr, "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
		require.Contains(t, msgStr, "image/jpeg")
	})
}

func TestWriteSingleBody(t *testing.T) {
	t.Parallel()

	t.Run("writes text/plain body", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer
		err := writeSingleBody(&buf, "text/plain", "Hello World")
		require.NoError(t, err)

		result := buf.String()
		require.Contains(t, result, "Content-Type: text/plain; charset=utf-8\r\n")
		require.Contains(t, result, "Content-Transfer-Encoding: quoted-printable\r\n")
		require.Contains(t, result, "Hello World")
	})

	t.Run("writes text/html body", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer
		err := writeSingleBody(&buf, "text/html", "<p>Hello</p>")
		require.NoError(t, err)

		result := buf.String()
		require.Contains(t, result, "Content-Type: text/html; charset=utf-8\r\n")
		require.Contains(t, result, "<p>Hello</p>")
	})

	t.Run("quoted-printable encodes long lines", func(t *testing.T) {
		t.Parallel()

		longLine := strings.Repeat("a", 100)
		var buf bytes.Buffer
		err := writeSingleBody(&buf, "text/plain", longLine)
		require.NoError(t, err)

		result := buf.String()
		// Quoted-printable uses soft line breaks (=) for lines over 76 chars
		require.Contains(t, result, "=")
	})

	t.Run("encodes special characters", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer
		err := writeSingleBody(&buf, "text/plain", "Special: é ñ ü")
		require.NoError(t, err)

		result := buf.String()
		// Special chars should be Q-encoded
		require.Contains(t, result, "=")
	})
}

func TestWriteAlternativeBody(t *testing.T) {
	t.Parallel()

	t.Run("creates multipart/alternative with text and HTML", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer
		err := writeAlternativeBody(&buf, "Plain text", "<p>HTML text</p>")
		require.NoError(t, err)

		result := buf.String()
		require.Contains(t, result, "Content-Type: multipart/alternative; boundary=")
		require.Contains(t, result, "Content-Type: text/plain; charset=utf-8\r\n")
		require.Contains(t, result, "Content-Type: text/html; charset=utf-8\r\n")
		require.Contains(t, result, "Plain text")
		require.Contains(t, result, "<p>HTML text</p>")
	})

	t.Run("text part comes before HTML part", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer
		err := writeAlternativeBody(&buf, "Text version", "<b>HTML version</b>")
		require.NoError(t, err)

		result := buf.String()
		textPos := strings.Index(result, "Text version")
		htmlPos := strings.Index(result, "HTML version")
		require.Greater(t, htmlPos, textPos, "HTML should appear after text in alternative")
	})
}

func TestWriteMixedBody(t *testing.T) {
	t.Parallel()

	t.Run("HTML and attachment", func(t *testing.T) {
		t.Parallel()

		email := &mailer.Email{
			HTML: "<p>Body</p>",
			Attachments: []mailer.Attachment{
				{
					Filename:    "file.txt",
					ContentType: "text/plain",
					Content:     []byte("content"),
				},
			},
		}

		var buf bytes.Buffer
		err := writeMixedBody(&buf, email, true, false)
		require.NoError(t, err)

		result := buf.String()
		require.Contains(t, result, "Content-Type: multipart/mixed; boundary=")
		require.Contains(t, result, "Content-Type: text/html; charset=utf-8\r\n")
		require.Contains(t, result, "Content-Type: text/plain; name=\"file.txt\"\r\n")
	})

	t.Run("text and attachment", func(t *testing.T) {
		t.Parallel()

		email := &mailer.Email{
			Text: "Plain body",
			Attachments: []mailer.Attachment{
				{
					Filename: "doc.pdf",
					Content:  []byte("pdf"),
				},
			},
		}

		var buf bytes.Buffer
		err := writeMixedBody(&buf, email, false, true)
		require.NoError(t, err)

		result := buf.String()
		require.Contains(t, result, "Content-Type: multipart/mixed; boundary=")
		require.Contains(t, result, "Content-Type: text/plain; charset=utf-8\r\n")
		require.Contains(t, result, "name=\"doc.pdf\"")
	})

	t.Run("HTML+text and attachment (nested alternative)", func(t *testing.T) {
		t.Parallel()

		email := &mailer.Email{
			HTML: "<p>HTML</p>",
			Text: "Text",
			Attachments: []mailer.Attachment{
				{
					Filename: "file.bin",
					Content:  []byte("data"),
				},
			},
		}

		var buf bytes.Buffer
		err := writeMixedBody(&buf, email, true, true)
		require.NoError(t, err)

		result := buf.String()
		require.Contains(t, result, "Content-Type: multipart/mixed; boundary=")
		require.Contains(t, result, "Content-Type: multipart/alternative; boundary=")
		require.Contains(t, result, "Content-Type: text/plain; charset=utf-8\r\n")
		require.Contains(t, result, "Content-Type: text/html; charset=utf-8\r\n")
		require.Contains(t, result, "name=\"file.bin\"")
	})
}

func TestWriteAttachment(t *testing.T) {
	t.Parallel()

	t.Run("regular attachment", func(t *testing.T) {
		t.Parallel()

		att := &mailer.Attachment{
			Filename:    "document.pdf",
			ContentType: "application/pdf",
			Content:     []byte("fake pdf content"),
		}

		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		err := writeAttachment(w, att)
		require.NoError(t, err)

		w.Close()
		result := buf.String()
		require.Contains(t, result, "Content-Type: application/pdf; name=\"document.pdf\"\r\n")
		require.Contains(t, result, "Content-Transfer-Encoding: base64\r\n")
		require.Contains(t, result, "Content-Disposition: attachment; filename=\"document.pdf\"\r\n")
		require.NotContains(t, result, "Content-ID")
	})

	t.Run("inline attachment with ContentID", func(t *testing.T) {
		t.Parallel()

		att := &mailer.Attachment{
			Filename:    "logo.png",
			ContentType: "image/png",
			ContentID:   "logo-cid-123",
			Content:     []byte("png data"),
		}

		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		err := writeAttachment(w, att)
		require.NoError(t, err)

		w.Close()
		result := buf.String()
		require.Contains(t, result, "Content-Type: image/png; name=\"logo.png\"\r\n")
		require.Contains(t, result, "Content-Disposition: inline; filename=\"logo.png\"\r\n")
		// MIME headers are case-insensitive, check for Content-Id variant
		require.Contains(t, result, "Content-Id: <logo-cid-123>\r\n")
	})

	t.Run("ContentID with pre-existing angle brackets is sanitized", func(t *testing.T) {
		t.Parallel()

		att := &mailer.Attachment{
			Filename:    "logo.png",
			ContentType: "image/png",
			ContentID:   "<logo-cid-123>",
			Content:     []byte("png data"),
		}

		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		err := writeAttachment(w, att)
		require.NoError(t, err)

		w.Close()
		result := buf.String()
		// Angle brackets supplied by the caller must not be doubled up.
		require.Contains(t, result, "Content-Id: <logo-cid-123>\r\n")
		require.NotContains(t, result, "<<logo-cid-123>>")
	})

	t.Run("defaults to octet-stream when ContentType empty", func(t *testing.T) {
		t.Parallel()

		att := &mailer.Attachment{
			Filename: "unknown.bin",
			Content:  []byte("binary"),
		}

		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		err := writeAttachment(w, att)
		require.NoError(t, err)

		w.Close()
		result := buf.String()
		require.Contains(t, result, "Content-Type: application/octet-stream; name=\"unknown.bin\"\r\n")
	})

	t.Run("base64 encodes content", func(t *testing.T) {
		t.Parallel()

		att := &mailer.Attachment{
			Filename:    "data.txt",
			ContentType: "text/plain",
			Content:     []byte("Hello World"),
		}

		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		err := writeAttachment(w, att)
		require.NoError(t, err)

		w.Close()
		result := buf.String()
		// "Hello World" base64 encoded is "SGVsbG8gV29ybGQ="
		require.Contains(t, result, "SGVsbG8gV29ybGQ=")
	})
}

func TestWriteHeader(t *testing.T) {
	t.Parallel()

	t.Run("writes header with CRLF", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer
		writeHeader(&buf, "X-Test", "value")
		require.Equal(t, "X-Test: value\r\n", buf.String())
	})

	t.Run("handles empty value", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer
		writeHeader(&buf, "X-Empty", "")
		require.Equal(t, "X-Empty: \r\n", buf.String())
	})

	t.Run("multiple headers", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer
		writeHeader(&buf, "From", "sender@example.com")
		writeHeader(&buf, "To", "recipient@example.com")
		writeHeader(&buf, "Subject", "Test")

		result := buf.String()
		require.Contains(t, result, "From: sender@example.com\r\n")
		require.Contains(t, result, "To: recipient@example.com\r\n")
		require.Contains(t, result, "Subject: Test\r\n")
	})
}

func TestBuildMessage_DateAndMessageIDHeaders(t *testing.T) {
	t.Parallel()

	t.Run("includes a parseable Date header", func(t *testing.T) {
		t.Parallel()

		email := &mailer.Email{
			Subject: "Test",
			Text:    "body",
			To:      []string{"recipient@example.com"},
		}

		msg, err := buildMessage("sender@example.com", email)
		require.NoError(t, err)

		hdr := parseHeaders(t, msg)
		dateVal := hdr.Get("Date")
		require.NotEmpty(t, dateVal, "Date header must be present")

		// Must parse as an RFC 1123Z timestamp.
		parsed, err := time.Parse(time.RFC1123Z, dateVal)
		require.NoError(t, err, "Date header %q must be RFC 1123Z", dateVal)
		require.WithinDuration(t, time.Now(), parsed, time.Minute)
	})

	t.Run("includes a well-formed Message-ID header", func(t *testing.T) {
		t.Parallel()

		email := &mailer.Email{
			Subject: "Test",
			Text:    "body",
			To:      []string{"recipient@example.com"},
		}

		msg, err := buildMessage("Sender Name <sender@example.com>", email)
		require.NoError(t, err)

		hdr := parseHeaders(t, msg)
		mid := hdr.Get("Message-ID")
		require.NotEmpty(t, mid, "Message-ID header must be present")
		require.True(t, strings.HasPrefix(mid, "<"), "Message-ID must start with <")
		require.True(t, strings.HasSuffix(mid, ">"), "Message-ID must end with >")
		// Domain is derived from the sender address.
		require.True(t, strings.HasSuffix(mid, "@example.com>"),
			"Message-ID %q should use the sender domain", mid)
		require.NotContains(t, strings.TrimSuffix(strings.TrimPrefix(mid, "<"), ">"), " ",
			"Message-ID local/domain parts must not contain spaces")
	})

	t.Run("Message-IDs are unique across messages", func(t *testing.T) {
		t.Parallel()

		email := &mailer.Email{
			Subject: "Test",
			Text:    "body",
			To:      []string{"recipient@example.com"},
		}

		msg1, err := buildMessage("sender@example.com", email)
		require.NoError(t, err)
		msg2, err := buildMessage("sender@example.com", email)
		require.NoError(t, err)

		id1 := parseHeaders(t, msg1).Get("Message-ID")
		id2 := parseHeaders(t, msg2).Get("Message-ID")
		require.NotEmpty(t, id1)
		require.NotEqual(t, id1, id2, "each message must get a unique Message-ID")
	})
}

func TestMessageID(t *testing.T) {
	t.Parallel()

	t.Run("derives domain from a plain address", func(t *testing.T) {
		t.Parallel()

		mid := messageID("user@example.org")
		require.True(t, strings.HasSuffix(mid, "@example.org>"))
	})

	t.Run("derives domain from a display-name address", func(t *testing.T) {
		t.Parallel()

		mid := messageID(`"Doe, John" <john@example.com>`)
		require.True(t, strings.HasSuffix(mid, "@example.com>"))
	})

	t.Run("falls back to localhost on unparseable address", func(t *testing.T) {
		t.Parallel()

		mid := messageID("not-an-address")
		require.True(t, strings.HasSuffix(mid, "@localhost>"))
	})
}

// parseHeaders extracts the RFC 5322 headers from a built message for assertion.
func parseHeaders(t *testing.T, msg []byte) mail.Header {
	t.Helper()
	m, err := mail.ReadMessage(bytes.NewReader(msg))
	require.NoError(t, err)
	return m.Header
}
