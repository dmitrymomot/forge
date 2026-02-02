package session

import (
	"context"
	"time"
)

// Store defines the interface for session persistence.
// Implementations handle storage-specific operations like
// database queries or cache lookups.
type Store interface {
	// Create persists a new session.
	Create(ctx context.Context, s *Session) error

	// Get retrieves a session by its token.
	// Returns ErrNotFound if the session doesn't exist.
	// Returns ErrExpired if the session has expired.
	Get(ctx context.Context, token string) (*Session, error)

	// Update saves changes to an existing session.
	Update(ctx context.Context, s *Session) error

	// Delete removes a session by its ID.
	Delete(ctx context.Context, id string) error

	// DeleteByUserID removes all sessions for a user.
	// Useful for "logout from all devices" functionality.
	DeleteByUserID(ctx context.Context, userID string) error

	// Touch updates the LastActiveAt timestamp without loading the full session.
	// Used for activity tracking without full session updates.
	Touch(ctx context.Context, id string, lastActiveAt time.Time) error
}
