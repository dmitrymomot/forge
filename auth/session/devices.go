package session

import (
	"context"
	"slices"

	"github.com/dmitrymomot/forge/core/id"
)

// ListByUser returns every live session for userID, newest first. Expired records the reaper has not removed yet are filtered out here, so the promise holds regardless of the driver. IP, user agent, and last-seen are columns, so a device list decodes no payload. Returns ErrUnsupported when the store has no user index. With a configured scope hook, results are confined to the resolved tenant.
func (m *Manager) ListByUser(ctx context.Context, userID string) ([]Record, error) {
	if m.index == nil {
		return nil, ErrUnsupported
	}
	if userID == "" {
		return nil, ErrAnonymous
	}
	tenant, err := m.resolveScope(ctx)
	if err != nil {
		return nil, err
	}
	recs, err := m.index.ListByUser(ctx, tenant, userID)
	if err != nil {
		return nil, err
	}
	now := m.now()
	return slices.DeleteFunc(recs, func(r Record) bool {
		return !r.ExpiresAt.IsZero() && !r.ExpiresAt.After(now)
	}), nil
}

// Revoke removes one of userID's sessions. It is user-bound: passing another user's session id is a no-op, not a cross-account logout. With a configured scope hook, it is also tenant-bound the same way.
func (m *Manager) Revoke(ctx context.Context, userID string, sessionID id.UUID) error {
	if m.index == nil {
		return ErrUnsupported
	}
	if userID == "" {
		return ErrAnonymous
	}
	tenant, err := m.resolveScope(ctx)
	if err != nil {
		return err
	}
	return m.index.DeleteOne(ctx, tenant, userID, sessionID)
}

// LogoutOthers removes every session for the bound user except this one, confined to the resolved tenant when a scope hook is configured.
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
	tenant, err := m.resolveScope(ctx)
	if err != nil {
		return err
	}
	return m.index.DeleteByUser(ctx, tenant, s.rec.UserID, s.rec.ID)
}

// DeleteByUser removes every session for userID — the GDPR erasure path. With a configured scope hook, it is confined to the resolved tenant.
func (m *Manager) DeleteByUser(ctx context.Context, userID string) error {
	if m.index == nil {
		return ErrUnsupported
	}
	if userID == "" {
		return ErrAnonymous
	}
	tenant, err := m.resolveScope(ctx)
	if err != nil {
		return err
	}
	return m.index.DeleteByUser(ctx, tenant, userID)
}

// DeleteExpired reaps expired records. Returns ErrUnsupported for stores whose backend expires records natively. It is deliberately GLOBAL and unscoped: expiry is storage hygiene, not a tenant isolation boundary.
func (m *Manager) DeleteExpired(ctx context.Context) (int, error) {
	if m.expirer == nil {
		return 0, ErrUnsupported
	}
	return m.expirer.DeleteExpired(ctx, m.now())
}
