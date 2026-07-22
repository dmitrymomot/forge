package email

import "errors"

var (
	// ErrInvalidConfig is returned by New when the Config fails validation.
	ErrInvalidConfig = errors.New("email: invalid config")

	// ErrInvalidMessage means the message cannot be sent as constructed: an
	// unparseable or missing address, no recipients, no body, a CR/LF in a
	// header value (injection), or a custom header that collides with one the
	// encoder owns.
	ErrInvalidMessage = errors.New("email: invalid message")

	// ErrTLSUnavailable means the server did not advertise STARTTLS while the
	// sender requires it (the default). Credentials are never sent and mail is
	// never submitted over plaintext in STARTTLS mode — fail closed.
	ErrTLSUnavailable = errors.New("email: server does not support STARTTLS")

	// ErrTransient means the server answered a 4xx SMTP status — a temporary
	// condition (greylisting, full mailbox, rate limit) worth retrying.
	ErrTransient = errors.New("email: transient SMTP failure")

	// ErrPermanent means the server answered a 5xx SMTP status — the message
	// or recipient is refused; retrying the same send won't help.
	ErrPermanent = errors.New("email: permanent SMTP failure")

	// ErrTemplateNotFound is returned by Render when the parsed set defines no
	// subject/html/text blocks under the requested name.
	ErrTemplateNotFound = errors.New("email: template not found")

	// ErrInvalidTemplate means a rendered template violates the message
	// contract: an empty subject, a newline in the subject, or a name that
	// defines a subject but neither body.
	ErrInvalidTemplate = errors.New("email: invalid template")
)
