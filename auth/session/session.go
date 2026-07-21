package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/core/id"
	"github.com/dmitrymomot/forge/core/random"
	"github.com/dmitrymomot/forge/ops/logger"
	"github.com/dmitrymomot/forge/web/fingerprint"
)

// tokenBytes is the entropy of a bearer token (base64url-encoded to 43
// chars). 256 bits matches OWASP guidance with a wide margin.
const tokenBytes = 32

// Session is one live session. Token is the bearer credential to transport to
// the client (cookie value); it is empty until the first Save and replaced by
// Rotate. ID is a stable identifier that survives rotation — use it for
// logging and device listings, never as a credential. Mutate UserID/Data and
// call Save to persist.
//
// IP, UserAgent, and LastSeenAt are display metadata for a "manage devices"
// UI: the request-level helpers (SaveRequest and friends) stamp IP/UserAgent
// from the request, and every Save refreshes LastSeenAt. Mark the current
// device by comparing a listed ID with the caller's own session ID.
type Session[T any] struct {
	CreatedAt  time.Time
	ExpiresAt  time.Time
	LastSeenAt time.Time
	Token      string
	UserID     string
	IP         string
	UserAgent  string
	Data       T
	fp         fingerprint.Digest
	ID         id.UUID
}

// Manager drives the session lifecycle over a pluggable Store. T is the
// consumer's session payload, JSON-encoded at rest. Create one with New.
type Manager[T any] struct {
	store Store
	cfg   config
}

// New builds a Manager over store. Defaults: 24h idle TTL, 30-day absolute
// lifetime, no fingerprint checking, no tenancy scoping.
func New[T any](store Store, opts ...Option) (*Manager[T], error) {
	cfg := config{
		clock:      clock.System(),
		logger:     logger.NewNope(),
		clientInfo: defaultClientInfo,
		ttl:        24 * time.Hour,
		lifetime:   30 * 24 * time.Hour,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.digest == nil {
		cfg.digest = func(ctx context.Context) (fingerprint.Digest, bool) {
			res, ok := fingerprint.FromContext(ctx)
			if !ok || res.Fingerprint.Hash == "" {
				return fingerprint.Digest{}, false
			}
			return res.Fingerprint.Digest(), true
		}
	}
	switch {
	case store == nil:
		return nil, fmt.Errorf("%w: store is required", ErrInvalidConfig)
	case cfg.ttl <= 0:
		return nil, fmt.Errorf("%w: ttl must be positive", ErrInvalidConfig)
	case cfg.lifetime < cfg.ttl:
		return nil, fmt.Errorf("%w: lifetime must be >= ttl", ErrInvalidConfig)
	case cfg.fpMode < 0 || cfg.fpMode > Strict:
		return nil, fmt.Errorf("%w: unknown fingerprint mode", ErrInvalidConfig)
	case cfg.clock == nil:
		return nil, fmt.Errorf("%w: clock must not be nil", ErrInvalidConfig)
	case cfg.logger == nil:
		return nil, fmt.Errorf("%w: logger must not be nil", ErrInvalidConfig)
	case cfg.clientInfo == nil:
		return nil, fmt.Errorf("%w: client info hook must not be nil", ErrInvalidConfig)
	}
	return &Manager[T]{store: store, cfg: cfg}, nil
}

// Start creates a fresh anonymous session. Nothing is persisted and Token is
// empty until Save — abandoned starts cost no storage. When fingerprinting is
// enabled and the request carries one, the current digest becomes the
// session's hijack-detection baseline.
func (m *Manager[T]) Start(ctx context.Context) *Session[T] {
	now := m.cfg.clock.Now()
	s := &Session[T]{ID: id.NewUUID(), CreatedAt: now, ExpiresAt: m.deadline(now, now)}
	if m.cfg.fpMode != 0 {
		if d, ok := m.cfg.digest(ctx); ok {
			s.fp = d
		}
	}
	return s
}

// Load resolves token to its session. It fails with ErrNotFound for unknown
// or out-of-scope tokens, ErrExpired past either deadline (the record is
// deleted), and — in Strict mode — ErrFingerprintMismatch on drift (the
// record is revoked). Warn mode logs drift and lets the session through.
func (m *Manager[T]) Load(ctx context.Context, token string) (*Session[T], error) {
	if token == "" {
		return nil, ErrNotFound
	}
	scope, err := m.resolveScope(ctx)
	if err != nil {
		return nil, err
	}
	rec, err := m.store.Load(ctx, token)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, errors.Join(ErrStore, err)
	}
	if rec.Scope != scope {
		// Cross-scope tokens are indistinguishable from unknown ones, and the
		// record is left alone: a guessed token must not let one tenant
		// revoke another's session.
		return nil, ErrNotFound
	}
	if !rec.ExpiresAt.After(m.cfg.clock.Now()) {
		// Defense-in-depth for stores without native TTL; best-effort delete,
		// the record is unusable either way.
		_ = m.store.Delete(ctx, token)
		return nil, ErrExpired
	}
	if err := m.checkFingerprint(ctx, token, rec); err != nil {
		return nil, err
	}
	var data T
	if len(rec.Data) > 0 {
		if err := json.Unmarshal(rec.Data, &data); err != nil {
			return nil, errors.Join(ErrCodec, err)
		}
	}
	return &Session[T]{
		ID:         rec.ID,
		CreatedAt:  rec.CreatedAt,
		ExpiresAt:  rec.ExpiresAt,
		LastSeenAt: rec.LastSeenAt,
		Token:      token,
		UserID:     rec.UserID,
		IP:         rec.IP,
		UserAgent:  rec.UserAgent,
		Data:       data,
		fp:         rec.Fingerprint,
	}, nil
}

// Save persists s and slides its idle deadline, stamping the current scope.
// The first Save mints the bearer token; afterwards s.Token is current and
// must be re-transported to the client whenever it changed (stateless stores
// change it on every Save). Saving a session past its absolute lifetime fails
// with ErrExpired.
func (m *Manager[T]) Save(ctx context.Context, s *Session[T]) error {
	scope, err := m.resolveScope(ctx)
	if err != nil {
		return err
	}
	now := m.cfg.clock.Now()
	deadline := m.deadline(s.CreatedAt, now)
	if !deadline.After(now) {
		return ErrExpired
	}
	data, err := json.Marshal(s.Data)
	if err != nil {
		return errors.Join(ErrCodec, err)
	}
	if s.Token == "" {
		s.Token = random.URLSafe(tokenBytes)
	}
	rec := Record{
		ID:          s.ID,
		UserID:      s.UserID,
		Scope:       scope,
		IP:          s.IP,
		UserAgent:   s.UserAgent,
		Data:        data,
		Fingerprint: s.fp,
		CreatedAt:   s.CreatedAt,
		ExpiresAt:   deadline,
		LastSeenAt:  now,
	}
	token, err := m.store.Save(ctx, s.Token, rec)
	if err != nil {
		return errors.Join(ErrStore, err)
	}
	s.Token = token
	s.ExpiresAt = deadline
	s.LastSeenAt = now
	return nil
}

// Rotate re-keys s: a fresh token is saved and the old one revoked, keeping
// ID, CreatedAt, and the absolute deadline. Call it on every privilege
// change — before that Save an attacker-planted token would become an
// authenticated session (session fixation). When fingerprinting is enabled
// the baseline is re-captured from the current request.
func (m *Manager[T]) Rotate(ctx context.Context, s *Session[T]) error {
	oldFP := s.fp
	if m.cfg.fpMode != 0 {
		if d, ok := m.cfg.digest(ctx); ok {
			s.fp = d
		}
	}
	old := s.Token
	s.Token = ""
	if err := m.Save(ctx, s); err != nil {
		// Roll the session back to exactly what the store still holds — the
		// old token AND the old fingerprint baseline, or a later successful
		// Save would persist a baseline the rotation never established.
		s.Token = old
		s.fp = oldFP
		return err
	}
	if old != "" && old != s.Token {
		// The old token must die with the rotation: a Delete failure is
		// reported so the caller can retry rather than leave two live tokens.
		if err := m.store.Delete(ctx, old); err != nil {
			return errors.Join(ErrStore, err)
		}
	}
	return nil
}

// Authenticate binds userID to s and rotates the token — the login (and
// privilege-change) helper. The zero UserID stays reserved for anonymous
// sessions, so an empty userID fails with ErrInvalidInput.
func (m *Manager[T]) Authenticate(ctx context.Context, s *Session[T], userID string) error {
	if userID == "" {
		return fmt.Errorf("%w: empty user id", ErrInvalidInput)
	}
	prev := s.UserID
	s.UserID = userID
	if err := m.Rotate(ctx, s); err != nil {
		s.UserID = prev
		return err
	}
	return nil
}

// Destroy revokes s and zeroes it. Destroying a never-saved or already-gone
// session is a no-op; a token belonging to another scope fails with
// ErrNotFound and stays alive. With stateless stores (cookiestore)
// revocation is impossible server-side — clearing the client's cookie is the
// real destroy.
func (m *Manager[T]) Destroy(ctx context.Context, s *Session[T]) error {
	if s.Token == "" {
		*s = Session[T]{}
		return nil
	}
	scope, err := m.resolveScope(ctx)
	if err != nil {
		return err
	}
	rec, err := m.store.Load(ctx, s.Token)
	switch {
	case errors.Is(err, ErrNotFound):
		// Already revoked elsewhere; zeroing below is all that's left.
	case err != nil:
		return errors.Join(ErrStore, err)
	case rec.Scope != scope:
		// The scope owning the record is the only one allowed to revoke it.
		return ErrNotFound
	default:
		if err := m.store.Delete(ctx, s.Token); err != nil {
			return errors.Join(ErrStore, err)
		}
	}
	*s = Session[T]{}
	return nil
}

// ListUserSessions returns the live sessions bound to userID within the
// current scope, newest first — the "manage devices" listing. Tokens are
// stored only as digests, so returned sessions carry an empty Token. Fails
// with ErrNoUserIndex when the Store lacks the UserIndex extension.
func (m *Manager[T]) ListUserSessions(ctx context.Context, userID string) ([]Session[T], error) {
	if userID == "" {
		return nil, fmt.Errorf("%w: empty user id", ErrInvalidInput)
	}
	ui, ok := m.store.(UserIndex)
	if !ok {
		return nil, ErrNoUserIndex
	}
	scope, err := m.resolveScope(ctx)
	if err != nil {
		return nil, err
	}
	recs, err := ui.ListByUser(ctx, scope, userID)
	if err != nil {
		return nil, errors.Join(ErrStore, err)
	}
	now := m.cfg.clock.Now()
	out := make([]Session[T], 0, len(recs))
	for _, rec := range recs {
		if !rec.ExpiresAt.After(now) {
			continue
		}
		var data T
		if len(rec.Data) > 0 {
			if err := json.Unmarshal(rec.Data, &data); err != nil {
				return nil, errors.Join(ErrCodec, err)
			}
		}
		out = append(out, Session[T]{
			ID:         rec.ID,
			CreatedAt:  rec.CreatedAt,
			ExpiresAt:  rec.ExpiresAt,
			LastSeenAt: rec.LastSeenAt,
			UserID:     rec.UserID,
			IP:         rec.IP,
			UserAgent:  rec.UserAgent,
			Data:       data,
			fp:         rec.Fingerprint,
		})
	}
	return out, nil
}

// DeleteUserSessions revokes every session bound to userID within the
// current scope — "log out everywhere" and GDPR deletion. Fails with
// ErrNoUserIndex when the Store lacks the UserIndex extension.
func (m *Manager[T]) DeleteUserSessions(ctx context.Context, userID string) error {
	return m.deleteUserSessions(ctx, userID)
}

// LogoutOthers revokes every other session of s's user — "log out other
// devices" — leaving s alive. It requires an authenticated, saved session.
func (m *Manager[T]) LogoutOthers(ctx context.Context, s *Session[T]) error {
	if s.UserID == "" {
		return fmt.Errorf("%w: anonymous session", ErrInvalidInput)
	}
	if s.Token == "" {
		return fmt.Errorf("%w: session was never saved", ErrInvalidInput)
	}
	return m.deleteUserSessions(ctx, s.UserID, s.ID)
}

// RevokeUserSession revokes the single session sessionID belonging to userID
// within the current scope — the "revoke this device" button. Revoking an
// absent (or someone else's) session is a no-op. Fails with ErrNoUserIndex
// when the Store lacks the UserIndex extension.
func (m *Manager[T]) RevokeUserSession(ctx context.Context, userID string, sessionID id.UUID) error {
	if userID == "" {
		return fmt.Errorf("%w: empty user id", ErrInvalidInput)
	}
	if sessionID.IsZero() {
		return fmt.Errorf("%w: zero session id", ErrInvalidInput)
	}
	ui, ok := m.store.(UserIndex)
	if !ok {
		return ErrNoUserIndex
	}
	scope, err := m.resolveScope(ctx)
	if err != nil {
		return err
	}
	if err := ui.DeleteOne(ctx, scope, userID, sessionID); err != nil {
		return errors.Join(ErrStore, err)
	}
	return nil
}

func (m *Manager[T]) deleteUserSessions(ctx context.Context, userID string, keep ...id.UUID) error {
	if userID == "" {
		return fmt.Errorf("%w: empty user id", ErrInvalidInput)
	}
	ui, ok := m.store.(UserIndex)
	if !ok {
		return ErrNoUserIndex
	}
	scope, err := m.resolveScope(ctx)
	if err != nil {
		return err
	}
	if err := ui.DeleteByUser(ctx, scope, userID, keep...); err != nil {
		return errors.Join(ErrStore, err)
	}
	return nil
}

// resolveScope runs the configured scope hook, failing closed: a hook error
// or empty scope aborts the operation so a scoped session can never land in
// an unscoped (or another tenant's) namespace.
func (m *Manager[T]) resolveScope(ctx context.Context) (string, error) {
	if m.cfg.scope == nil {
		return "", nil
	}
	s, err := m.cfg.scope(ctx)
	if err != nil {
		return "", errors.Join(ErrScope, err)
	}
	if s == "" {
		return "", ErrScope
	}
	return s, nil
}

// deadline computes the next expiry: idle TTL from now, capped by the
// absolute lifetime from createdAt.
func (m *Manager[T]) deadline(createdAt, now time.Time) time.Time {
	idle := now.Add(m.cfg.ttl)
	if abs := createdAt.Add(m.cfg.lifetime); abs.Before(idle) {
		return abs
	}
	return idle
}

// checkFingerprint applies the configured mode. Sessions without a stored
// baseline are never checked; with one, a missing live digest counts as a
// mismatch (fail closed in Strict).
func (m *Manager[T]) checkFingerprint(ctx context.Context, token string, rec Record) error {
	if m.cfg.fpMode == 0 || rec.Fingerprint.Hash == "" {
		return nil
	}
	cur, ok := m.cfg.digest(ctx)
	if ok && cur.Hash == rec.Fingerprint.Hash {
		return nil
	}
	if m.cfg.fpMode == Warn {
		if m.cfg.logger.Enabled(ctx, slog.LevelWarn) {
			m.cfg.logger.WarnContext(ctx, "session: fingerprint drift",
				slog.String("session_id", rec.ID.String()),
				slog.Any("components", fingerprint.Drift(rec.Fingerprint, cur)))
		}
		return nil
	}
	// Strict: revoke before reporting so the token cannot be replayed from a
	// better-spoofed client. A failed revoke must not swallow the mismatch —
	// callers branching on ErrFingerprintMismatch (security alerts, forced
	// logout) still need the signal.
	if err := m.store.Delete(ctx, token); err != nil {
		return errors.Join(ErrFingerprintMismatch, ErrStore, err)
	}
	return ErrFingerprintMismatch
}
