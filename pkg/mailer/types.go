package mailer

import "net/mail"

// Tags represents email tags/categories that can be either presence-only
// (using struct{}{}) or key-value pairs (using string values).
// This abstraction works across different email providers:
//   - Postmark: uses only tag names
//   - Resend: uses name-value pairs (presence-only tags become name="true")
type Tags map[string]any

// SimpleTags creates presence-only tags from a list of tag names.
// These are converted to appropriate format by each provider adapter.
func SimpleTags(names ...string) Tags {
	t := make(Tags, len(names))
	for _, n := range names {
		t[n] = struct{}{}
	}
	return t
}

// Recipient formats a name and email into RFC 5322 address format.
// Returns just the email if no name is provided. Display names containing
// special characters (commas, quotes, etc.) are properly quoted/encoded per
// RFC 5322 via mail.Address.String, so names like "Doe, John" do not break
// downstream address parsing.
func Recipient(name, email string) string {
	if name == "" {
		return email
	}
	addr := mail.Address{Name: name, Address: email}
	return addr.String()
}

// Email represents a fully-prepared email message ready for sending.
type Email struct {
	Headers     map[string]string // Custom headers
	Tags        Tags              // Provider-specific tags/categories
	Subject     string            // Email subject
	HTML        string            // HTML body content
	Text        string            // Plain text alternative
	From        string            // Override default sender (if provider allows)
	ReplyTo     string            // Reply-to address
	To          []string          // Recipients (at least one required)
	CC          []string          // Carbon copy recipients
	BCC         []string          // Blind carbon copy recipients
	Attachments []Attachment      // File attachments
}

// Attachment represents an email attachment.
type Attachment struct {
	Filename    string // Display name for the attachment
	ContentType string // MIME type (e.g., "application/pdf")
	ContentID   string // Optional Content-ID for inline attachments
	Content     []byte // Raw file content
}
