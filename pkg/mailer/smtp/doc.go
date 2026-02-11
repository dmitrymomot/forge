// Package smtp provides an SMTP adapter that implements mailer.Sender using only the Go standard library.
//
// This adapter is suitable for local development (Mailpit) and production use with any SMTP server.
// It requires no external dependencies beyond stdlib and supports STARTTLS and SMTP AUTH PLAIN.
//
// # Basic Usage
//
// Create an SMTP sender and use it with the mailer:
//
//	import (
//		"context"
//		"embed"
//
//		"github.com/dmitrymomot/forge/pkg/mailer"
//		"github.com/dmitrymomot/forge/pkg/mailer/smtp"
//	)
//
//	//go:embed templates/*.md
//	var templates embed.FS
//
//	func main() {
//		ctx := context.Background()
//
//		// Create SMTP sender with Mailpit defaults (localhost:1025)
//		sender := smtp.New(smtp.Config{
//			SenderEmail: "team@example.com",
//			SenderName:  "Team",
//		})
//
//		// Create renderer with your embedded templates
//		renderer := mailer.NewRenderer(templates, mailer.RendererConfig{})
//		m := mailer.New(sender, renderer, mailer.Config{
//			FallbackSubject: "Notification",
//		})
//
//		// Send email
//		err := m.Send(ctx, mailer.SendParams{
//			To:       "user@example.com",
//			Template: "templates/welcome.md",
//			Data:     map[string]any{"Name": "John"},
//		})
//		if err != nil {
//			panic(err)
//		}
//	}
//
// # Configuration
//
// The Config struct uses env struct tags compatible with caarlos0/env:
//
//	type Config struct {
//		Host        string `env:"HOST"       envDefault:"localhost"`
//		Username    string `env:"USERNAME"`
//		Password    string `env:"PASSWORD"`
//		SenderEmail string `env:"FROM_EMAIL"`
//		SenderName  string `env:"FROM_NAME"`
//		Port        int    `env:"PORT"       envDefault:"1025"`
//		TLS         bool   `env:"TLS"        envDefault:"false"`
//	}
//
// Default configuration targets Mailpit running on localhost:1025 with no authentication.
//
// # Production Setup
//
// For production SMTP servers, configure authentication and TLS:
//
//	import "os"
//
//	sender := smtp.New(smtp.Config{
//		Host:        "smtp.example.com",
//		Port:        587,
//		Username:    os.Getenv("SMTP_USERNAME"),
//		Password:    os.Getenv("SMTP_PASSWORD"),
//		TLS:         true,
//		SenderEmail: "noreply@example.com",
//		SenderName:  "Example Team",
//	})
//
// # MIME Support
//
// The adapter handles all MIME encoding automatically:
//
//   - Quoted-printable encoding for text/plain and text/html parts
//   - Base64 encoding for attachments
//   - Multipart/alternative when both text and HTML are present
//   - Multipart/mixed when attachments are included
//   - RFC 5322 address formatting with display names
//   - Content-ID for inline images (use ContentID field in Attachment)
//
// # Authentication and TLS
//
// STARTTLS is used when TLS is true. SMTP AUTH PLAIN is used when both Username and Password are provided.
// For unauthenticated SMTP servers (like Mailpit), leave Username and Password empty.
package smtp
