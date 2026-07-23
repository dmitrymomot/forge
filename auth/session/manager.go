package session

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log/slog"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/core/id"
	"github.com/dmitrymomot/forge/ops/logger"
)

// tokenBytes is the entropy of a minted session token.
const tokenBytes = 32

// Manager owns session lifecycle and storage. It knows nothing about HTTP:
// no requests, no transports, no policies. Safe for concurrent use.
type Manager struct {
	store   Store
	toucher Toucher                               // nil when the store lacks the capability
	index   UserIndex                             // nil when the store lacks the capability
	expirer Expirer                               // nil when the store lacks the capability
	scope   func(context.Context) (string, error) // nil when unscoped (single-tenant)
	clk     clock.Clock
	log     *slog.Logger
	cfg     Config
}

// New builds a Manager. Store capabilities are detected once, here — a missing
// capability that a configured option requires is a boot error, not a surprise
// at the first request.
func New(cfg Config, opts ...Option) (*Manager, error) {
	c := config{Config: cfg, clock: clock.System(), logger: logger.NewNope()}
	for _, opt := range opts {
		opt(&c)
	}
	if c.store == nil {
		return nil, ErrNoStore
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}

	m := &Manager{store: c.store, clk: c.clock, log: c.logger, cfg: c.Config, scope: c.scope}
	m.toucher, _ = c.store.(Toucher)
	m.index, _ = c.store.(UserIndex)
	m.expirer, _ = c.store.(Expirer)

	if m.cfg.Touch > 0 && m.toucher == nil {
		return nil, ErrTouchUnsupported
	}
	return m, nil
}

func (m *Manager) now() time.Time { return m.clk.Now().UTC() }

// resolveScope runs the configured scope hook, failing closed: a hook error or
// an empty scope from a configured hook aborts the operation, so a scoped
// session can never land in an unscoped (or another tenant's) bucket. With no
// hook it returns "" — the single-tenant path, no allocation, no call.
func (m *Manager) resolveScope(ctx context.Context) (string, error) {
	if m.scope == nil {
		return "", nil
	}
	s, err := m.scope(ctx)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrScope, err)
	}
	if s == "" {
		return "", ErrScope
	}
	return s, nil
}

// Start returns a fresh anonymous session. It performs no I/O and mints no
// row: storage is touched on the first save.
func (m *Manager) Start() *Session {
	now := m.now()
	rec := Record{
		ID:         id.NewUUID(),
		CreatedAt:  now,
		LastSeenAt: now,
	}
	rec.ExpiresAt = m.deadline(rec, now)
	return newSession(rec, newToken(), true, m.now)
}

// Load fetches the session for token. An expired record yields ErrExpired and
// is deleted; a missing one yields ErrNotFound. With a configured scope hook,
// a record belonging to another tenant also yields ErrNotFound — the same
// error as truly-not-found, so tenant existence cannot be probed.
func (m *Manager) Load(ctx context.Context, token string) (*Session, error) {
	rec, err := m.store.Load(ctx, token)
	if err != nil {
		return nil, err
	}
	now := m.now()
	if !rec.ExpiresAt.IsZero() && !rec.ExpiresAt.After(now) {
		if err := m.store.Delete(ctx, token); err != nil {
			m.log.WarnContext(ctx, "session: deleting expired record failed", slog.Any("error", err))
		}
		return nil, ErrExpired
	}

	if m.scope != nil {
		tenant, err := m.resolveScope(ctx)
		if err != nil {
			return nil, err
		}
		if rec.Tenant != tenant {
			return nil, ErrNotFound
		}
	}

	storedSeenAt := rec.LastSeenAt
	rec.LastSeenAt = now
	rec.ExpiresAt = m.deadline(rec, now)
	sess := newSession(rec, token, false, m.now)
	sess.storedSeenAt = storedSeenAt
	return sess, nil
}

// Save persists the session, encoding any dirty namespaces first. The token the
// store returns becomes the session's token, which is what lets a stateless
// store re-encode the record into a fresh credential. With a configured scope
// hook, the tenant is resolved and stamped before anything else is touched, so
// a hook error or empty scope aborts before any mutation or write.
func (m *Manager) Save(ctx context.Context, s *Session) error {
	if s == nil {
		return ErrNoSession
	}
	tenant, err := m.resolveScope(ctx)
	if err != nil {
		return err
	}
	s.rec.Tenant = tenant
	if err := s.encode(); err != nil {
		return err
	}
	now := m.now()
	s.rec.LastSeenAt = now
	s.rec.ExpiresAt = m.deadline(s.rec, now)

	tok, err := m.store.Save(ctx, s.token, s.rec)
	if err != nil {
		return err
	}
	s.token = tok
	s.isNew = false
	s.clearDirty()
	s.syncInfo()
	return nil
}

// deadline is the effective expiry: the sliding idle window, capped by the
// absolute lifetime when one is configured. A zero cap means no cap: with
// MaxTTL/RememberMax at zero, the cap branch never runs and continuous
// activity keeps the session alive indefinitely.
func (m *Manager) deadline(rec Record, now time.Time) time.Time {
	idle, maxTTL := m.cfg.Idle, m.cfg.MaxTTL
	if rec.Remembered {
		idle, maxTTL = m.cfg.RememberIdle, m.cfg.RememberMax
	}
	exp := now.Add(idle)
	if maxTTL > 0 {
		if capped := rec.CreatedAt.Add(maxTTL); capped.Before(exp) {
			return capped
		}
	}
	return exp
}

// touchDue reports whether enough time has passed since the LastSeenAt that
// was actually persisted to justify a metadata-only write. It compares
// against storedSeenAt, not rec.LastSeenAt: Load refreshes the in-memory
// LastSeenAt to now, so comparing against that would make the elapsed
// interval always zero and no request would ever touch.
func (m *Manager) touchDue(s *Session, now time.Time) bool {
	if m.cfg.Touch <= 0 || m.toucher == nil || s.isNew {
		return false
	}
	return now.Sub(s.storedSeenAt) >= m.cfg.Touch
}

// newToken mints a fresh, unguessable session credential.
func newToken() string {
	b := make([]byte, tokenBytes)
	if _, err := rand.Read(b); err != nil {
		panic("session: crypto/rand failed: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// AuthOption tunes Authenticate.
type AuthOption func(*authOptions)

type authOptions struct{ remember bool }

// Remember selects the remember-me deadline pair and marks the record so a
// transport can choose persistent client storage. Taking the bool directly
// keeps call sites free of conditional option slices.
func Remember(v bool) AuthOption { return func(o *authOptions) { o.remember = v } }

// Authenticate binds userID to the session and rotates the token, which is
// mandatory: reusing the pre-login credential is the session fixation bug. The
// session ID, CreatedAt, and payload survive; a failed save rolls every field
// back so the client is never left holding a credential no record answers to.
func (m *Manager) Authenticate(ctx context.Context, s *Session, userID string, opts ...AuthOption) error {
	if s == nil {
		return ErrNoSession
	}
	if userID == "" {
		return ErrAnonymous
	}
	var o authOptions
	for _, opt := range opts {
		opt(&o)
	}

	oldToken, oldRec := s.token, s.rec
	now := m.now()

	s.rec.UserID = userID
	s.rec.Remembered = o.remember
	s.rec.ElevatedAt = now
	s.token = newToken()

	wasNew := s.isNew
	if err := m.Save(ctx, s); err != nil {
		s.token, s.rec = oldToken, oldRec
		return err
	}
	if !wasNew {
		if err := m.store.Delete(ctx, oldToken); err != nil {
			m.log.WarnContext(ctx, "session: deleting pre-rotation record failed", slog.Any("error", err))
		}
	}
	return nil
}

// Rotate issues a fresh token for the same session, preserving every field.
func (m *Manager) Rotate(ctx context.Context, s *Session) error {
	if s == nil {
		return ErrNoSession
	}
	oldToken := s.token
	s.token = newToken()
	if err := m.Save(ctx, s); err != nil {
		s.token = oldToken
		return err
	}
	if oldToken != s.token {
		if err := m.store.Delete(ctx, oldToken); err != nil {
			m.log.WarnContext(ctx, "session: deleting pre-rotation record failed", slog.Any("error", err))
		}
	}
	return nil
}

// Destroy removes the record. The caller's transport clears the credential.
func (m *Manager) Destroy(ctx context.Context, s *Session) error {
	if s == nil {
		return ErrNoSession
	}
	if err := m.store.Delete(ctx, s.token); err != nil {
		return err
	}
	s.deleted = true
	return nil
}

// Elevate stamps a successful identity re-proof. Session records that it
// happened; auth/access decides what it entitles the user to.
func (m *Manager) Elevate(ctx context.Context, s *Session) error {
	if s == nil {
		return ErrNoSession
	}
	s.rec.ElevatedAt = m.now()
	return m.Save(ctx, s)
}

// Bind carries the pinned device metadata Rebind writes.
type Bind struct {
	IP          string
	UserAgent   string
	Fingerprint string
}

// Rebind replaces the pinned metadata. This is the deliberate re-pin after a
// successful re-authentication — the middleware never does it implicitly,
// because a per-request refresh would let a stolen credential overwrite the
// binding on its first use and match forever after.
func (m *Manager) Rebind(ctx context.Context, s *Session, b Bind) error {
	if s == nil {
		return ErrNoSession
	}
	s.rec.IP, s.rec.UserAgent, s.rec.Fingerprint = b.IP, b.UserAgent, b.Fingerprint
	return m.Save(ctx, s)
}
