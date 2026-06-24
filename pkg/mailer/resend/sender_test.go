package resend

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	goresend "github.com/resend/resend-go/v3"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/pkg/mailer"
)

// capturingTransport is an in-process http.RoundTripper that records the
// outgoing request payload and returns a canned response. It lets the tests
// assert how the Sender maps a mailer.Email onto Resend's API without any
// network access.
type capturingTransport struct {
	method     string
	path       string
	authHeader string
	body       []byte

	status   int
	respBody string
	err      error
}

func (c *capturingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	c.method = req.Method
	c.path = req.URL.Path
	c.authHeader = req.Header.Get("Authorization")
	if req.Body != nil {
		b, _ := io.ReadAll(req.Body)
		c.body = b
	}
	if c.err != nil {
		return nil, c.err
	}

	status := c.status
	if status == 0 {
		status = http.StatusOK
	}
	respBody := c.respBody
	if respBody == "" {
		respBody = `{"id":"email-123"}`
	}
	header := make(http.Header)
	header.Set("Content-Type", "application/json")
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(respBody)),
		Header:     header,
		Request:    req,
	}, nil
}

// newTestSender wires a Sender to a capturing transport so the real Resend HTTP
// client is exercised end-to-end, but every request is intercepted in-process.
func newTestSender(t *testing.T, cfg Config) (*Sender, *capturingTransport) {
	t.Helper()
	rt := &capturingTransport{}
	s := New(cfg)
	s.client = goresend.NewCustomClient(&http.Client{Transport: rt}, cfg.APIKey)
	return s, rt
}

// decodeRequest parses the captured JSON payload into the Resend request type.
func decodeRequest(t *testing.T, body []byte) goresend.SendEmailRequest {
	t.Helper()
	var req goresend.SendEmailRequest
	require.NoError(t, json.NewDecoder(bytes.NewReader(body)).Decode(&req))
	return req
}

func TestSend(t *testing.T) {
	t.Parallel()

	t.Run("maps core fields onto the Resend request", func(t *testing.T) {
		t.Parallel()

		s, rt := newTestSender(t, Config{APIKey: "re_test_key"})

		err := s.Send(context.Background(), &mailer.Email{
			To:      []string{"a@example.com", "b@example.com"},
			CC:      []string{"cc@example.com"},
			BCC:     []string{"bcc@example.com"},
			Subject: "Hello",
			HTML:    "<p>Hi</p>",
			Text:    "Hi",
			ReplyTo: "reply@example.com",
			Headers: map[string]string{"X-Entity": "42"},
			From:    "Sender <sender@example.com>",
		})
		require.NoError(t, err)

		require.Equal(t, http.MethodPost, rt.method)
		require.Equal(t, "/emails", rt.path)
		require.Equal(t, "Bearer re_test_key", rt.authHeader)

		req := decodeRequest(t, rt.body)
		require.Equal(t, "Sender <sender@example.com>", req.From)
		require.Equal(t, []string{"a@example.com", "b@example.com"}, req.To)
		require.Equal(t, []string{"cc@example.com"}, req.Cc)
		require.Equal(t, []string{"bcc@example.com"}, req.Bcc)
		require.Equal(t, "Hello", req.Subject)
		require.Equal(t, "<p>Hi</p>", req.Html)
		require.Equal(t, "Hi", req.Text)
		require.Equal(t, "reply@example.com", req.ReplyTo)
		require.Equal(t, "42", req.Headers["X-Entity"])
	})

	t.Run("uses config sender name and email when From is empty", func(t *testing.T) {
		t.Parallel()

		s, rt := newTestSender(t, Config{
			APIKey:      "re_key",
			SenderEmail: "noreply@example.com",
			SenderName:  "Acme",
		})

		err := s.Send(context.Background(), &mailer.Email{
			To:      []string{"user@example.com"},
			Subject: "Welcome",
			HTML:    "<p>Welcome</p>",
		})
		require.NoError(t, err)

		req := decodeRequest(t, rt.body)
		require.Equal(t, `"Acme" <noreply@example.com>`, req.From)
	})

	t.Run("quotes config sender name containing a comma", func(t *testing.T) {
		t.Parallel()

		s, rt := newTestSender(t, Config{
			APIKey:      "re_key",
			SenderEmail: "noreply@example.com",
			SenderName:  "Doe, John",
		})

		err := s.Send(context.Background(), &mailer.Email{
			To:      []string{"user@example.com"},
			Subject: "Hi",
			HTML:    "<p>Hi</p>",
		})
		require.NoError(t, err)

		req := decodeRequest(t, rt.body)
		require.Equal(t, `"Doe, John" <noreply@example.com>`, req.From)
	})

	t.Run("falls back to bare sender email when no name is set", func(t *testing.T) {
		t.Parallel()

		s, rt := newTestSender(t, Config{
			APIKey:      "re_key",
			SenderEmail: "noreply@example.com",
		})

		err := s.Send(context.Background(), &mailer.Email{
			To:      []string{"user@example.com"},
			Subject: "Hi",
			HTML:    "<p>Hi</p>",
		})
		require.NoError(t, err)

		req := decodeRequest(t, rt.body)
		require.Equal(t, "noreply@example.com", req.From)
	})

	t.Run("maps attachments", func(t *testing.T) {
		t.Parallel()

		s, rt := newTestSender(t, Config{APIKey: "re_key", SenderEmail: "s@example.com"})

		err := s.Send(context.Background(), &mailer.Email{
			To:      []string{"user@example.com"},
			Subject: "With Attachment",
			HTML:    "<p>See attached</p>",
			Attachments: []mailer.Attachment{
				{
					Filename:    "logo.png",
					ContentType: "image/png",
					ContentID:   "logo-1",
					Content:     []byte("png-bytes"),
				},
			},
		})
		require.NoError(t, err)

		// Assert the raw wire payload Resend receives (its Attachment uses a
		// custom MarshalJSON with snake_case keys and an int-array content), so
		// decode the JSON generically rather than round-tripping the typed value.
		var raw struct {
			Attachments []struct {
				Filename    string `json:"filename"`
				ContentType string `json:"content_type"`
				ContentID   string `json:"content_id"`
				Content     []int  `json:"content"`
			} `json:"attachments"`
		}
		require.NoError(t, json.Unmarshal(rt.body, &raw))
		require.Len(t, raw.Attachments, 1)
		require.Equal(t, "logo.png", raw.Attachments[0].Filename)
		require.Equal(t, "image/png", raw.Attachments[0].ContentType)
		require.Equal(t, "logo-1", raw.Attachments[0].ContentID)
		require.Equal(t, []byte("png-bytes"), intsToBytes(raw.Attachments[0].Content))
	})

	t.Run("maps tags including the tagValue switch", func(t *testing.T) {
		t.Parallel()

		s, rt := newTestSender(t, Config{APIKey: "re_key", SenderEmail: "s@example.com"})

		tags := mailer.Tags{
			"presence": struct{}{},
			"campaign": "summer",
			"flag":     true,
			"count":    7,
		}

		err := s.Send(context.Background(), &mailer.Email{
			To:      []string{"user@example.com"},
			Subject: "Tagged",
			HTML:    "<p>Tagged</p>",
			Tags:    tags,
		})
		require.NoError(t, err)

		req := decodeRequest(t, rt.body)
		require.Len(t, req.Tags, 4)

		got := make(map[string]string, len(req.Tags))
		for _, tag := range req.Tags {
			got[tag.Name] = tag.Value
		}
		require.Equal(t, "true", got["presence"])
		require.Equal(t, "summer", got["campaign"])
		require.Equal(t, "true", got["flag"])
		require.Equal(t, "7", got["count"])
	})

	t.Run("wraps API errors", func(t *testing.T) {
		t.Parallel()

		s, rt := newTestSender(t, Config{APIKey: "re_key", SenderEmail: "s@example.com"})
		rt.status = http.StatusUnprocessableEntity
		rt.respBody = `{"statusCode":422,"message":"invalid recipient","name":"validation_error"}`

		err := s.Send(context.Background(), &mailer.Email{
			To:      []string{"user@example.com"},
			Subject: "Boom",
			HTML:    "<p>Boom</p>",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "resend: failed to send email")
		require.Contains(t, err.Error(), "invalid recipient")
	})
}

func TestTagValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   any
		want string
	}{
		{"presence struct", struct{}{}, "true"},
		{"nil", nil, "true"},
		{"string", "hello", "hello"},
		{"bool true", true, "true"},
		{"bool false", false, "false"},
		{"int", 42, "42"},
		{"int64", int64(9000), "9000"},
		{"float64", 3.5, "3.5"},
		{"stringer", stringerVal{}, "stringer-value"},
		{"fallback", []int{1, 2}, "[1 2]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, tagValue(tt.in))
		})
	}
}

type stringerVal struct{}

func (stringerVal) String() string { return "stringer-value" }

// intsToBytes converts the int-array content (Resend's wire format for
// attachment bytes) back into a byte slice for comparison.
func intsToBytes(in []int) []byte {
	out := make([]byte, len(in))
	for i, v := range in {
		out[i] = byte(v)
	}
	return out
}
