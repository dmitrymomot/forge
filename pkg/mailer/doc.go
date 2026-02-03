// Package mailer provides a universal email sending interface with template rendering.
//
// The package separates email sending (via providers) from template rendering,
// allowing easy swapping of email providers while keeping the same template system.
//
// # Architecture
//
// The package consists of three main components:
//
//   - Sender: Interface that email providers implement
//   - Renderer: Converts markdown templates with YAML frontmatter to HTML
//   - Mailer: High-level client combining Sender and Renderer
//
// # Usage
//
// Basic usage with the built-in Resend provider:
//
//	import (
//		"context"
//		"os"
//
//		"github.com/dmitrymomot/forge/pkg/mailer"
//		"github.com/dmitrymomot/forge/pkg/mailer/resend"
//	)
//
//	func main() {
//		ctx := context.Background()
//
//		// Create the provider
//		sender := resend.New(resend.Config{
//			APIKey:      os.Getenv("RESEND_API_KEY"),
//			SenderEmail: "team@example.com",
//			SenderName:  "Team",
//		})
//
//		// Create the renderer with embedded templates
//		renderer := mailer.NewRenderer(emails.FS)
//
//		// Create the mailer
//		m := mailer.New(sender, renderer, mailer.Config{
//			FallbackSubject: "Notification",
//			DefaultLayout:   "base.html",
//		})
//
//		// Send templated email
//		err := m.Send(ctx, mailer.SendParams{
//			To:       "user@example.com",
//			Template: "welcome.md",
//			Data:     map[string]any{"Name": "John"},
//		})
//		if err != nil {
//			panic(err)
//		}
//	}
//
// # Templates
//
// Templates are markdown files with optional YAML frontmatter:
//
//	---
//	Subject: Welcome {{.Name}}!
//	---
//
//	# Welcome
//
//	Hello {{.Name}}, welcome to our service!
//
//	[!button|Get Started]({{.URL}})
//
// Subject fields support Go template syntax ({{.Variable}}) for dynamic subjects.
//
// # Sending Emails
//
// Mailer provides two methods for sending emails:
//
//   - Send: Renders a template and sends the email
//   - SendRaw: Sends a pre-built Email without rendering
//
// SendParams supports optional overrides for subject, layout, sender, reply-to,
// CC, BCC, and attachments.
//
// # Email Tags
//
// The Email type supports provider-specific tags for categorization:
//
//	email := &mailer.Email{
//		To:      []string{"user@example.com"},
//		Subject: "Welcome",
//		HTML:    "<p>Hello!</p>",
//		Tags:    mailer.SimpleTags("welcome", "onboarding"),
//	}
//
// # Custom Providers
//
// Implement the Sender interface to add support for other email providers:
//
//	type MySender struct{}
//
//	func (s *MySender) Send(ctx context.Context, email *mailer.Email) error {
//		// Send email using your provider's API
//		return nil
//	}
//
//	// Use with mailer
//	m := mailer.New(&MySender{}, renderer, cfg)
//
// # Background Jobs
//
// Users create their own typed tasks for background email delivery:
//
//	type SendWelcomeTask struct {
//		mailer *mailer.Mailer
//	}
//
//	type WelcomePayload struct {
//		Email string
//		Name  string
//	}
//
//	func (t *SendWelcomeTask) Name() string { return "send_welcome" }
//
//	func (t *SendWelcomeTask) Handle(ctx context.Context, p WelcomePayload) error {
//		return t.mailer.Send(ctx, mailer.SendParams{
//			To:       p.Email,
//			Template: "welcome.md",
//			Data:     p,
//		})
//	}
//
// # Errors
//
// The package defines several error variables for specific failure cases:
//
//   - ErrNoRecipient: No recipient specified
//   - ErrNoSubject: No subject provided
//   - ErrNoContent: No HTML content provided
//   - ErrTemplateNotFound: Template file not found
//   - ErrLayoutNotFound: Layout file not found
//   - ErrRenderFailed: Template rendering failed
//   - ErrSendFailed: Email sending failed
//   - ErrInvalidFrontmatter: Invalid YAML frontmatter
package mailer
