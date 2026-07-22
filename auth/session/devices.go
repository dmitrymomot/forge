package session

import (
	"context"

	"github.com/dmitrymomot/forge/core/id"
)

// ListByUser returns every live session for userID, newest first. IP, user agent, and last-seen are columns, so a device list decodes no payload. Returns ErrUnsupported when the store has no user index.
func (m *Manager) ListByUser(ctx context.Context, userID string) ([]Record, error) {
	if m.index == nil {
		return nil, ErrUnsupported
	}
	if userID == "" {
		return nil, ErrAnonymous
	}
	return m.index.ListByUser(ctx, userID)
}

// Revoke removes one of userID's sessions. It is user-bound: passing another user's session id is a no-op, not a cross-account logout.
func (m *Manager) Revoke(ctx context.Context, userID string, sessionID id.UUID) error {
	if m.index == nil {
		return ErrUnsupported
	}
	if userID == "" {
		return ErrAnonymous
	}
	return m.index.DeleteOne(ctx, userID, sessionID)
}

// LogoutOthers removes every session for the bound user except this one.
func (m *Manager) LogoutOthers(ctx context.Context, s *Session) error {
	if m.index == nil {
		return ErrUnsupported
	}
	if s == nil {
		return ErrNoSession
	}
	if s.rec.UserID == "" {
		return ErrAnonymous
	}
	return m.index.DeleteByUser(ctx, s.rec.UserID, s.rec.ID)
}

// DeleteByUser removes every session for userID — the GDPR erasure path.
func (m *Manager) DeleteByUser(ctx context.Context, userID string) error {
	if m.index == nil {
		return ErrUnsupported
	}
	if userID == "" {
		return ErrAnonymous
	}
	return m.index.DeleteByUser(ctx, userID)
}

// DeleteExpired reaps expired records. Returns ErrUnsupported for stores whose backend expires records natively.
func (m *Manager) DeleteExpired(ctx context.Context) (int, error) {
	if m.expirer == nil {
		return 0, ErrUnsupported
	}
	return m.expirer.DeleteExpired(ctx, m.now())
}
