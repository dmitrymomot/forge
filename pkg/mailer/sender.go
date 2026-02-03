package mailer

import "context"

// Sender defines the minimal interface that email providers must implement.
// It accepts a fully-prepared Email and handles the actual delivery.
type Sender interface {
	// Send delivers an email message.
	// The Email must have To, Subject, and HTML already set.
	// Returns an error if delivery fails.
	Send(ctx context.Context, email *Email) error
}
