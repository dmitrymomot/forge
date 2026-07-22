package email

import "context"

// Sender delivers one message — the package's composition seam. The SMTP
// type is the stdlib implementation; provider adapters (SES, Postmark, …)
// implement the same interface over Message.Encode or their JSON APIs and
// stay consumer-side. Durable delivery rides async/queue with a Sender as
// the job handler's deliverer: ErrTransient means retry, ErrPermanent means
// dead-letter.
type Sender interface {
	Send(ctx context.Context, msg Message) error
}
