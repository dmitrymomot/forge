package session

import (
	"context"
	"crypto/rand"
	"encoding/base64"
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
	toucher Toucher   // nil when the store lacks the capability
	index   UserIndex // nil when the store lacks the capability
	expirer Expirer   // nil when the store lacks the capability
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

	m := &Manager{store: c.store, clk: c.clock, log: c.logger, cfg: c.Config}
	m.toucher, _ = c.store.(Toucher)
	m.index, _ = c.store.(UserIndex)
	m.expirer, _ = c.store.(Expirer)

	if m.cfg.Touch > 0 && m.toucher == nil {
		return nil, ErrTouchUnsupported
	}
	return m, nil
}

func (m *Manager) now() time.Time { return m.clk.Now().UTC() }

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
// is deleted; a missing one yields ErrNotFound.
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

	storedSeenAt := rec.LastSeenAt
	rec.LastSeenAt = now
	rec.ExpiresAt = m.deadline(rec, now)
	sess := newSession(rec, token, false, m.now)
	sess.storedSeenAt = storedSeenAt
	return sess, nil
}

// Save persists the session, encoding any dirty namespaces first. The token the
// store returns becomes the session's token, which is what lets a stateless
// store re-encode the record into a fresh credential.
func (m *Manager) Save(ctx context.Context, s *Session) error {
	if s == nil {
		return ErrNoSession
	}
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
