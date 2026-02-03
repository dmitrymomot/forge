package mailer

import (
	"context"
	"errors"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockSender is a mock implementation of Sender interface.
type MockSender struct {
	mock.Mock
}

func (m *MockSender) Send(ctx context.Context, email *Email) error {
	args := m.Called(ctx, email)
	return args.Error(0)
}

func TestMailer_Send_Success(t *testing.T) {
	t.Parallel()

	fs := fstest.MapFS{
		"layouts/base.html": &fstest.MapFile{
			Data: []byte(`<html><body>{{.Content}}</body></html>`),
		},
		"welcome.md": &fstest.MapFile{
			Data: []byte(`---
Subject: Welcome {{.Name}}
---
Hello **{{.Name}}**!
`),
		},
	}

	mockSender := &MockSender{}
	renderer := NewRendererWithConfig(fs, RendererConfig{LayoutDir: "layouts"})
	cfg := Config{
		DefaultLayout:   "base.html",
		FallbackSubject: "Notification",
	}
	mailer := New(mockSender, renderer, cfg)

	mockSender.On("Send", mock.Anything, mock.MatchedBy(func(email *Email) bool {
		return email.To[0] == "alice@example.com" &&
			email.Subject == "Welcome Alice" &&
			len(email.HTML) > 0 &&
			len(email.Text) > 0
	})).Return(nil)

	err := mailer.Send(context.Background(), SendParams{
		To:       "alice@example.com",
		Template: "welcome.md",
		Data:     map[string]string{"Name": "Alice"},
	})

	require.NoError(t, err)
	mockSender.AssertExpectations(t)
}

func TestMailer_Send_NoRecipient(t *testing.T) {
	t.Parallel()

	mockSender := &MockSender{}
	renderer := NewRenderer(fstest.MapFS{})
	mailer := New(mockSender, renderer, Config{})

	err := mailer.Send(context.Background(), SendParams{
		Template: "test.md",
		Data:     nil,
	})

	require.ErrorIs(t, err, ErrNoRecipient)
	mockSender.AssertNotCalled(t, "Send")
}

func TestMailer_Send_RenderFailure(t *testing.T) {
	t.Parallel()

	fs := fstest.MapFS{} // Empty filesystem
	mockSender := &MockSender{}
	renderer := NewRenderer(fs)
	mailer := New(mockSender, renderer, Config{DefaultLayout: "missing.html"})

	err := mailer.Send(context.Background(), SendParams{
		To:       "user@example.com",
		Template: "nonexistent.md",
		Data:     nil,
	})

	require.Error(t, err)
	require.ErrorIs(t, err, ErrRenderFailed)
	mockSender.AssertNotCalled(t, "Send")
}

func TestMailer_Send_SenderFailure(t *testing.T) {
	t.Parallel()

	fs := fstest.MapFS{
		"layouts/base.html": &fstest.MapFile{
			Data: []byte(`<html>{{.Content}}</html>`),
		},
		"test.md": &fstest.MapFile{
			Data: []byte(`Hello world`),
		},
	}

	mockSender := &MockSender{}
	renderer := NewRendererWithConfig(fs, RendererConfig{LayoutDir: "layouts"})
	mailer := New(mockSender, renderer, Config{
		DefaultLayout:   "base.html",
		FallbackSubject: "Test",
	})

	senderErr := errors.New("smtp connection failed")
	mockSender.On("Send", mock.Anything, mock.Anything).Return(senderErr)

	err := mailer.Send(context.Background(), SendParams{
		To:       "user@example.com",
		Template: "test.md",
		Data:     nil,
	})

	require.Error(t, err)
	require.ErrorIs(t, err, ErrSendFailed)
	require.ErrorIs(t, err, senderErr)
	mockSender.AssertExpectations(t)
}

func TestMailer_Send_SubjectResolution(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		paramsSubject   string
		templateContent string
		fallbackSubject string
		expectedSubject string
	}{
		{
			name:          "uses params subject when provided",
			paramsSubject: "Override Subject",
			templateContent: `---
Subject: Template Subject
---
Body`,
			fallbackSubject: "Fallback",
			expectedSubject: "Override Subject",
		},
		{
			name:          "uses template metadata when params empty",
			paramsSubject: "",
			templateContent: `---
Subject: Template Subject
---
Body`,
			fallbackSubject: "Fallback",
			expectedSubject: "Template Subject",
		},
		{
			name:            "uses fallback when both empty",
			paramsSubject:   "",
			templateContent: `Body without metadata`,
			fallbackSubject: "Fallback Subject",
			expectedSubject: "Fallback Subject",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fs := fstest.MapFS{
				"layouts/base.html": &fstest.MapFile{
					Data: []byte(`<html>{{.Content}}</html>`),
				},
				"test.md": &fstest.MapFile{
					Data: []byte(tt.templateContent),
				},
			}

			mockSender := &MockSender{}
			renderer := NewRendererWithConfig(fs, RendererConfig{LayoutDir: "layouts"})
			mailer := New(mockSender, renderer, Config{
				DefaultLayout:   "base.html",
				FallbackSubject: tt.fallbackSubject,
			})

			mockSender.On("Send", mock.Anything, mock.MatchedBy(func(email *Email) bool {
				return email.Subject == tt.expectedSubject
			})).Return(nil)

			err := mailer.Send(context.Background(), SendParams{
				To:       "user@example.com",
				Template: "test.md",
				Subject:  tt.paramsSubject,
				Data:     nil,
			})

			require.NoError(t, err)
			mockSender.AssertExpectations(t)
		})
	}
}

func TestMailer_Send_SubjectTemplating(t *testing.T) {
	t.Parallel()

	fs := fstest.MapFS{
		"layouts/base.html": &fstest.MapFile{
			Data: []byte(`<html>{{.Content}}</html>`),
		},
		"dynamic.md": &fstest.MapFile{
			Data: []byte(`---
Subject: "Order #{{.OrderID}} Confirmed"
---
Your order has been confirmed.
`),
		},
	}

	mockSender := &MockSender{}
	renderer := NewRendererWithConfig(fs, RendererConfig{LayoutDir: "layouts"})
	mailer := New(mockSender, renderer, Config{DefaultLayout: "base.html"})

	mockSender.On("Send", mock.Anything, mock.MatchedBy(func(email *Email) bool {
		return email.Subject == "Order #12345 Confirmed"
	})).Return(nil)

	err := mailer.Send(context.Background(), SendParams{
		To:       "customer@example.com",
		Template: "dynamic.md",
		Data:     map[string]string{"OrderID": "12345"},
	})

	require.NoError(t, err)
	mockSender.AssertExpectations(t)
}

func TestMailer_Send_SubjectTemplatingError(t *testing.T) {
	t.Parallel()

	fs := fstest.MapFS{
		"layouts/base.html": &fstest.MapFile{
			Data: []byte(`<html>{{.Content}}</html>`),
		},
		"test.md": &fstest.MapFile{
			Data: []byte(`Body`),
		},
	}

	mockSender := &MockSender{}
	renderer := NewRendererWithConfig(fs, RendererConfig{LayoutDir: "layouts"})
	mailer := New(mockSender, renderer, Config{
		DefaultLayout:   "base.html",
		FallbackSubject: "Invalid {{.Unclosed",
	})

	err := mailer.Send(context.Background(), SendParams{
		To:       "user@example.com",
		Template: "test.md",
		Data:     nil,
	})

	require.Error(t, err)
	require.ErrorIs(t, err, ErrRenderFailed)
	mockSender.AssertNotCalled(t, "Send")
}

func TestMailer_Send_WithOptionalFields(t *testing.T) {
	t.Parallel()

	fs := fstest.MapFS{
		"layouts/base.html": &fstest.MapFile{
			Data: []byte(`<html>{{.Content}}</html>`),
		},
		"test.md": &fstest.MapFile{
			Data: []byte(`Test email`),
		},
	}

	mockSender := &MockSender{}
	renderer := NewRendererWithConfig(fs, RendererConfig{LayoutDir: "layouts"})
	mailer := New(mockSender, renderer, Config{
		DefaultLayout:   "base.html",
		FallbackSubject: "Test",
	})

	mockSender.On("Send", mock.Anything, mock.MatchedBy(func(email *Email) bool {
		return email.To[0] == "user@example.com" &&
			email.From == "sender@example.com" &&
			email.ReplyTo == "reply@example.com" &&
			len(email.CC) == 1 && email.CC[0] == "cc@example.com" &&
			len(email.BCC) == 1 && email.BCC[0] == "bcc@example.com" &&
			len(email.Attachments) == 1 && email.Attachments[0].Filename == "doc.pdf"
	})).Return(nil)

	err := mailer.Send(context.Background(), SendParams{
		To:       "user@example.com",
		Template: "test.md",
		From:     "sender@example.com",
		ReplyTo:  "reply@example.com",
		CC:       []string{"cc@example.com"},
		BCC:      []string{"bcc@example.com"},
		Attachments: []Attachment{
			{Filename: "doc.pdf", Content: []byte("pdf content"), ContentType: "application/pdf"},
		},
		Data: nil,
	})

	require.NoError(t, err)
	mockSender.AssertExpectations(t)
}

func TestMailer_Send_CustomLayout(t *testing.T) {
	t.Parallel()

	fs := fstest.MapFS{
		"layouts/base.html": &fstest.MapFile{
			Data: []byte(`<html><body>{{.Content}}</body></html>`),
		},
		"layouts/custom.html": &fstest.MapFile{
			Data: []byte(`<div class="custom">{{.Content}}</div>`),
		},
		"test.md": &fstest.MapFile{
			Data: []byte(`Test`),
		},
	}

	mockSender := &MockSender{}
	renderer := NewRendererWithConfig(fs, RendererConfig{LayoutDir: "layouts"})
	mailer := New(mockSender, renderer, Config{
		DefaultLayout:   "base.html",
		FallbackSubject: "Test",
	})

	mockSender.On("Send", mock.Anything, mock.MatchedBy(func(email *Email) bool {
		// Check that custom layout was used
		return len(email.HTML) > 0 && email.HTML != ""
	})).Return(nil)

	err := mailer.Send(context.Background(), SendParams{
		To:       "user@example.com",
		Template: "test.md",
		Layout:   "custom.html",
		Data:     nil,
	})

	require.NoError(t, err)
	mockSender.AssertExpectations(t)
}

func TestMailer_SendRaw_Success(t *testing.T) {
	t.Parallel()

	mockSender := &MockSender{}
	mailer := New(mockSender, nil, Config{})

	email := &Email{
		To:      []string{"user@example.com"},
		Subject: "Test Subject",
		HTML:    "<p>Hello</p>",
		Text:    "Hello",
	}

	mockSender.On("Send", mock.Anything, email).Return(nil)

	err := mailer.SendRaw(context.Background(), email)

	require.NoError(t, err)
	mockSender.AssertExpectations(t)
}

func TestMailer_SendRaw_NoRecipient(t *testing.T) {
	t.Parallel()

	mockSender := &MockSender{}
	mailer := New(mockSender, nil, Config{})

	email := &Email{
		To:      []string{},
		Subject: "Test",
		HTML:    "<p>Hello</p>",
	}

	err := mailer.SendRaw(context.Background(), email)

	require.ErrorIs(t, err, ErrNoRecipient)
	mockSender.AssertNotCalled(t, "Send")
}

func TestMailer_SendRaw_NoSubject(t *testing.T) {
	t.Parallel()

	mockSender := &MockSender{}
	mailer := New(mockSender, nil, Config{})

	email := &Email{
		To:      []string{"user@example.com"},
		Subject: "",
		HTML:    "<p>Hello</p>",
	}

	err := mailer.SendRaw(context.Background(), email)

	require.ErrorIs(t, err, ErrNoSubject)
	mockSender.AssertNotCalled(t, "Send")
}

func TestMailer_SendRaw_NoContent(t *testing.T) {
	t.Parallel()

	mockSender := &MockSender{}
	mailer := New(mockSender, nil, Config{})

	email := &Email{
		To:      []string{"user@example.com"},
		Subject: "Test",
		HTML:    "",
	}

	err := mailer.SendRaw(context.Background(), email)

	require.ErrorIs(t, err, ErrNoContent)
	mockSender.AssertNotCalled(t, "Send")
}

func TestMailer_SendRaw_SenderFailure(t *testing.T) {
	t.Parallel()

	mockSender := &MockSender{}
	mailer := New(mockSender, nil, Config{})

	email := &Email{
		To:      []string{"user@example.com"},
		Subject: "Test",
		HTML:    "<p>Hello</p>",
	}

	senderErr := errors.New("network error")
	mockSender.On("Send", mock.Anything, email).Return(senderErr)

	err := mailer.SendRaw(context.Background(), email)

	require.Error(t, err)
	require.ErrorIs(t, err, ErrSendFailed)
	require.ErrorIs(t, err, senderErr)
	mockSender.AssertExpectations(t)
}
