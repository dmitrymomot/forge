package mailer

import "errors"

var (
	// ErrNoRecipient indicates no recipient was specified.
	ErrNoRecipient = errors.New("email must have at least one recipient")

	// ErrNoSubject indicates no subject was provided.
	ErrNoSubject = errors.New("email must have a subject")

	// ErrNoContent indicates no HTML content was provided.
	ErrNoContent = errors.New("email must have HTML content")

	// ErrTemplateNotFound indicates the template file was not found.
	ErrTemplateNotFound = errors.New("template not found")

	// ErrLayoutNotFound indicates the layout file was not found.
	ErrLayoutNotFound = errors.New("layout not found")

	// ErrRenderFailed indicates template rendering failed.
	ErrRenderFailed = errors.New("failed to render template")

	// ErrSendFailed indicates email sending failed.
	ErrSendFailed = errors.New("failed to send email")

	// ErrInvalidFrontmatter indicates invalid YAML frontmatter.
	ErrInvalidFrontmatter = errors.New("invalid frontmatter")
)
