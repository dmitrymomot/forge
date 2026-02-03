package mailer

import (
	"bytes"
	"context"
	"errors"
	texttemplate "text/template"
)

// Mailer provides high-level email sending with template rendering.
type Mailer struct {
	sender   Sender
	renderer *Renderer
	config   Config
}

// New creates a new Mailer with the given sender and renderer.
func New(sender Sender, renderer *Renderer, cfg Config) *Mailer {
	return &Mailer{
		sender:   sender,
		renderer: renderer,
		config:   cfg,
	}
}

// SendParams contains parameters for sending a templated email.
type SendParams struct {
	To       string // Single recipient (most common case)
	Template string // Template filename (e.g., "welcome.md")
	Data     any    // Template data

	// Optional overrides
	Subject     string       // Override template subject
	Layout      string       // Override default layout
	From        string       // Override default sender
	ReplyTo     string       // Reply-to address
	CC          []string     // Carbon copy
	BCC         []string     // Blind carbon copy
	Attachments []Attachment // File attachments
}

// Send renders a template and sends an email.
// Subject resolution: params.Subject > template metadata > config fallback.
func (m *Mailer) Send(ctx context.Context, params SendParams) error {
	if params.To == "" {
		return ErrNoRecipient
	}

	layout := params.Layout
	if layout == "" {
		layout = m.config.DefaultLayout
	}

	result, err := m.renderer.Render(layout, params.Template, params.Data)
	if err != nil {
		return errors.Join(ErrRenderFailed, err)
	}

	subject := params.Subject
	if subject == "" {
		if subjectFromMeta, ok := result.Metadata["Subject"].(string); ok {
			subject = subjectFromMeta
		} else {
			subject = m.config.FallbackSubject
		}
	}

	// Process subject as template (supports {{.Variable}} syntax)
	processedSubject, err := m.processSubject(subject, params.Data)
	if err != nil {
		return errors.Join(ErrRenderFailed, err)
	}

	email := &Email{
		To:          []string{params.To},
		Subject:     processedSubject,
		HTML:        result.HTML,
		Text:        result.Text,
		From:        params.From,
		ReplyTo:     params.ReplyTo,
		CC:          params.CC,
		BCC:         params.BCC,
		Attachments: params.Attachments,
	}

	if err := m.sender.Send(ctx, email); err != nil {
		return errors.Join(ErrSendFailed, err)
	}

	return nil
}

// SendRaw sends a pre-built email without template rendering.
func (m *Mailer) SendRaw(ctx context.Context, email *Email) error {
	if len(email.To) == 0 {
		return ErrNoRecipient
	}
	if email.Subject == "" {
		return ErrNoSubject
	}
	if email.HTML == "" {
		return ErrNoContent
	}

	if err := m.sender.Send(ctx, email); err != nil {
		return errors.Join(ErrSendFailed, err)
	}

	return nil
}

func (m *Mailer) processSubject(subject string, data any) (string, error) {
	tmpl, err := texttemplate.New("subject").Parse(subject)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}

	return buf.String(), nil
}
