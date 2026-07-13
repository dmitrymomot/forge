package oauthserver

import "errors"

var (
	// ErrClientNotFound is returned when a client id is unknown — including
	// when a tenancy-scoped call targets another tenant's client.
	ErrClientNotFound = errors.New("oauthserver: client not found")
	// ErrClientRevoked is returned by management calls on a revoked client.
	ErrClientRevoked = errors.New("oauthserver: client revoked")
	// ErrDuplicateClient is returned by Store.Create on an ID collision.
	ErrDuplicateClient = errors.New("oauthserver: duplicate client")
	// ErrInvalidConfig is returned by New/AuthorizeHandler for invalid setup.
	ErrInvalidConfig = errors.New("oauthserver: invalid config")
	// ErrInvalidInput is returned by CreateClient for invalid input.
	ErrInvalidInput = errors.New("oauthserver: invalid input")
)
