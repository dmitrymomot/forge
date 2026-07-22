package email_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/comms/email"
)

func validMessage() email.Message {
	return email.Message{
		From:    "Acme <no-reply@acme.example>",
		To:      []string{"ann@example.com"},
		Subject: "Hello",
		Text:    "Hi Ann,",
	}
}

func TestMessageValidation(t *testing.T) {
	t.Parallel()

	mutations := map[string]func(*email.Message){
		"missing from":            func(m *email.Message) { m.From = "" },
		"unparseable from":        func(m *email.Message) { m.From = "not an address" },
		"unparseable to":          func(m *email.Message) { m.To = []string{"@@nope"} },
		"unparseable cc":          func(m *email.Message) { m.Cc = []string{"broken<"} },
		"unparseable bcc":         func(m *email.Message) { m.Bcc = []string{"x y z"} },
		"no recipients":           func(m *email.Message) { m.To = nil },
		"no body":                 func(m *email.Message) { m.Text = "" },
		"newline in subject":      func(m *email.Message) { m.Subject = "hi\r\nBcc: spy@example.com" },
		"newline in header value": func(m *email.Message) { m.Headers = map[string]string{"X-Tag": "a\r\nX-Spy: b"} },
		"newline in header name":  func(m *email.Message) { m.Headers = map[string]string{"X-Tag\r\nX-Spy": "a"} },
		"empty header name":       func(m *email.Message) { m.Headers = map[string]string{"": "a"} },
		"reserved header":         func(m *email.Message) { m.Headers = map[string]string{"bcc": "spy@example.com"} },
		"reserved content type":   func(m *email.Message) { m.Headers = map[string]string{"content-type": "text/x"} },
		"attachment no filename":  func(m *email.Message) { m.Attachments = []email.Attachment{{Content: []byte("x")}} },
		"attachment bad filename": func(m *email.Message) {
			m.Attachments = []email.Attachment{{Filename: "a\"b.txt", Content: []byte("x")}}
		},
		// "Bcc:" and "Bcc " canonicalize to themselves, so they'd dodge the
		// reserved list and be re-parsed by receivers as a real Bcc header.
		"reserved header via colon": func(m *email.Message) { m.Headers = map[string]string{"Bcc:": "spy@example.com"} },
		"reserved header via space": func(m *email.Message) { m.Headers = map[string]string{"Bcc ": "spy@example.com"} },
		"non-ascii header name":     func(m *email.Message) { m.Headers = map[string]string{"X-Café": "x"} },
		"control in header value":   func(m *email.Message) { m.Headers = map[string]string{"X-Tag": "a\x00b"} },
		"attachment backslash filename": func(m *email.Message) {
			m.Attachments = []email.Attachment{{Filename: `report\`, Content: []byte("x")}}
		},
		"attachment overlong filename": func(m *email.Message) {
			m.Attachments = []email.Attachment{{Filename: strings.Repeat("a", 256) + ".txt", Content: []byte("x")}}
		},
		"attachment content type injection": func(m *email.Message) {
			m.Attachments = []email.Attachment{{Filename: "a.txt", ContentType: "text/plain\r\nX-Spy: 1", Content: []byte("x")}}
		},
		"inline attachment cid-unsafe filename": func(m *email.Message) {
			m.Attachments = []email.Attachment{{Filename: "my logo.png", Inline: true, Content: []byte("x")}}
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			msg := validMessage()
			mutate(&msg)
			err := msg.Encode(&bytes.Buffer{})
			require.ErrorIs(t, err, email.ErrInvalidMessage)
		})
	}
}

func TestMessageValidationAccepts(t *testing.T) {
	t.Parallel()

	t.Run("bcc-only recipients", func(t *testing.T) {
		t.Parallel()
		msg := validMessage()
		msg.To = nil
		msg.Bcc = []string{"hidden@example.com"}
		require.NoError(t, msg.Encode(&bytes.Buffer{}))
	})
	t.Run("html-only body", func(t *testing.T) {
		t.Parallel()
		msg := validMessage()
		msg.Text = ""
		msg.HTML = "<p>hi</p>"
		require.NoError(t, msg.Encode(&bytes.Buffer{}))
	})
	t.Run("custom non-reserved header", func(t *testing.T) {
		t.Parallel()
		msg := validMessage()
		msg.Headers = map[string]string{"X-Campaign": "onboarding"}
		var buf bytes.Buffer
		require.NoError(t, msg.Encode(&buf))
		assert.Contains(t, buf.String(), "X-Campaign: onboarding\r\n")
	})
	t.Run("empty subject", func(t *testing.T) {
		t.Parallel()
		msg := validMessage()
		msg.Subject = ""
		var buf bytes.Buffer
		require.NoError(t, msg.Encode(&buf))
		assert.NotContains(t, buf.String(), "Subject:")
	})
	t.Run("zero-length attachment content", func(t *testing.T) {
		t.Parallel()
		msg := validMessage()
		msg.Attachments = []email.Attachment{{Filename: "empty.txt"}}
		require.NoError(t, msg.Encode(&bytes.Buffer{}))
	})
}

func TestMessageEncodeCRLFOnly(t *testing.T) {
	t.Parallel()

	msg := validMessage()
	msg.HTML = "<p>Hi Ann,</p>\n<p>multi\nline</p>"
	msg.Text = "Hi Ann,\nmulti\nline"
	msg.Attachments = []email.Attachment{{Filename: "a.txt", Content: bytes.Repeat([]byte("data"), 100)}}
	var buf bytes.Buffer
	require.NoError(t, msg.Encode(&buf))

	raw := buf.Bytes()
	for i, b := range raw {
		if b == '\n' {
			require.Greater(t, i, 0, "output starts with bare LF")
			assert.Equal(t, byte('\r'), raw[i-1], "bare LF at offset %d: %q", i, raw[max(0, i-20):i+1])
		}
	}
	assert.False(t, strings.Contains(strings.ReplaceAll(string(raw), "\r\n", ""), "\r"), "stray CR")
}
