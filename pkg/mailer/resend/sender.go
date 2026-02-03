package resend

import (
	"context"
	"fmt"
	"strconv"

	"github.com/resend/resend-go/v3"

	"github.com/dmitrymomot/forge/pkg/mailer"
)

// Sender implements mailer.Sender using the Resend API.
type Sender struct {
	client *resend.Client
	config Config
}

// New creates a new Resend sender.
func New(cfg Config) *Sender {
	return &Sender{
		client: resend.NewClient(cfg.APIKey),
		config: cfg,
	}
}

// Send implements mailer.Sender.
func (s *Sender) Send(ctx context.Context, email *mailer.Email) error {
	from := email.From
	if from == "" {
		if s.config.SenderName != "" {
			from = fmt.Sprintf("%s <%s>", s.config.SenderName, s.config.SenderEmail)
		} else {
			from = s.config.SenderEmail
		}
	}

	req := &resend.SendEmailRequest{
		From:    from,
		To:      email.To,
		Subject: email.Subject,
		Html:    email.HTML,
		Text:    email.Text,
		ReplyTo: email.ReplyTo,
		Cc:      email.CC,
		Bcc:     email.BCC,
		Headers: email.Headers,
	}

	// Convert attachments
	if len(email.Attachments) > 0 {
		req.Attachments = s.convertAttachments(email.Attachments)
	}

	// Convert tags
	if len(email.Tags) > 0 {
		req.Tags = s.convertTags(email.Tags)
	}

	_, err := s.client.Emails.SendWithContext(ctx, req)
	if err != nil {
		return fmt.Errorf("resend: failed to send email: %w", err)
	}

	return nil
}

func (s *Sender) convertAttachments(attachments []mailer.Attachment) []*resend.Attachment {
	result := make([]*resend.Attachment, len(attachments))
	for i, a := range attachments {
		result[i] = &resend.Attachment{
			Filename:    a.Filename,
			Content:     a.Content,
			ContentType: a.ContentType,
			ContentId:   a.ContentID,
		}
	}
	return result
}

func (s *Sender) convertTags(tags mailer.Tags) []resend.Tag {
	result := make([]resend.Tag, 0, len(tags))
	for name, value := range tags {
		result = append(result, resend.Tag{
			Name:  name,
			Value: tagValue(value),
		})
	}
	return result
}

// tagValue converts any value to a string for Resend's tag API.
// Presence-only tags (struct{}{}) become "true".
func tagValue(v any) string {
	switch val := v.(type) {
	case nil, struct{}:
		return "true" // presence-only tag
	case string:
		return val
	case bool:
		return strconv.FormatBool(val)
	case int:
		return strconv.Itoa(val)
	case int64:
		return strconv.FormatInt(val, 10)
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	case fmt.Stringer:
		return val.String()
	default:
		return fmt.Sprint(val)
	}
}
