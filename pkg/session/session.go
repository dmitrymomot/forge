package session

import (
	"errors"
	"time"
)

// Session represents a user session with metadata and arbitrary values.
type Session struct {
	CreatedAt    time.Time
	LastActiveAt time.Time
	ExpiresAt    time.Time

	UserID      *string        // nil = anonymous session
	Values      map[string]any // Arbitrary session data
	ID          string         // Unique identifier (typically UUID)
	Token       string         // Cookie token (different from ID for security)
	IP          string         // Client IP address
	UserAgent   string         // Raw User-Agent header
	Device      string         // Parsed device info (e.g., "Chrome/128 (macOS, desktop)")
	Fingerprint string         // Device fingerprint for hijacking detection

	dirty bool // tracks if session needs saving
	isNew bool // tracks if session was just created
}

// New creates a new session with the given ID and token.
func New(id, token string, expiresAt time.Time) *Session {
	now := time.Now()
	return &Session{
		ID:           id,
		Token:        token,
		Values:       make(map[string]any),
		CreatedAt:    now,
		LastActiveAt: now,
		ExpiresAt:    expiresAt,
		isNew:        true,
		dirty:        true,
	}
}

// IsAuthenticated returns true if the session has an associated user.
func (s *Session) IsAuthenticated() bool {
	return s.UserID != nil && *s.UserID != ""
}

// SetValue stores a value in the session.
// Marks the session as dirty for automatic saving.
func (s *Session) SetValue(key string, val any) {
	if s.Values == nil {
		s.Values = make(map[string]any)
	}
	s.Values[key] = val
	s.dirty = true
}

// GetValue retrieves a value from the session.
func (s *Session) GetValue(key string) (any, bool) {
	if s.Values == nil {
		return nil, false
	}
	val, ok := s.Values[key]
	return val, ok
}

// DeleteValue removes a value from the session.
// Marks the session as dirty only if the key existed.
func (s *Session) DeleteValue(key string) {
	if s.Values == nil {
		return
	}
	if _, exists := s.Values[key]; exists {
		delete(s.Values, key)
		s.dirty = true
	}
}

// IsDirty returns true if the session has unsaved changes.
func (s *Session) IsDirty() bool {
	return s.dirty
}

// ClearDirty marks the session as clean (saved).
// Called by the session manager after persisting changes.
func (s *Session) ClearDirty() {
	s.dirty = false
}

// MarkDirty marks the session as needing to be saved.
func (s *Session) MarkDirty() {
	s.dirty = true
}

// IsNew returns true if the session was just created.
func (s *Session) IsNew() bool {
	return s.isNew
}

// ClearNew marks the session as no longer new.
// Called after the session is first persisted.
func (s *Session) ClearNew() {
	s.isNew = false
}

// IsExpired returns true if the session has expired.
func (s *Session) IsExpired() bool {
	return time.Now().After(s.ExpiresAt)
}

// Value is a typed helper to retrieve session values with type safety.
// Returns an error if the key doesn't exist or type assertion fails.
func Value[T any](s *Session, key string) (T, error) {
	var zero T
	if s == nil {
		return zero, ErrNotFound
	}

	val, ok := s.GetValue(key)
	if !ok {
		return zero, ErrNotFound
	}

	typed, ok := val.(T)
	if !ok {
		return zero, errors.New("session: type mismatch for key: " + key)
	}

	return typed, nil
}

// ValueOr is a typed helper that returns a default value if the key
// doesn't exist or type assertion fails.
func ValueOr[T any](s *Session, key string, defaultVal T) T {
	val, err := Value[T](s, key)
	if err != nil {
		return defaultVal
	}
	return val
}
