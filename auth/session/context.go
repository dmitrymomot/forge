package session

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/dmitrymomot/forge/core/ctxkey"
	"github.com/dmitrymomot/forge/core/id"
)

// Info is the small, always-available view of the current session. It is
// carried by pointer and updated in place, so a handler that authenticates
// mid-request does not leave a stale UserID for the rest of the chain. The
// payload is deliberately absent: reaching it requires the Manager.
type Info struct {
	CreatedAt  time.Time
	ExpiresAt  time.Time
	ElevatedAt time.Time
	UserID     string
	ID         id.UUID
}

// Authenticated reports whether a principal is bound.
func (i *Info) Authenticated() bool { return i != nil && i.UserID != "" }

var (
	sessionKey = ctxkey.New[*Session]("session")
	infoKey    = ctxkey.New[*Info]("session.info")
)

// FromContext returns the Info stored by Middleware.
func FromContext(ctx context.Context) (*Info, bool) { return infoKey.From(ctx) }

// For returns the session Middleware loaded for r. ok is false when the
// middleware is not mounted on this route.
func (m *Manager) For(r *http.Request) (*Session, bool) {
	if r == nil {
		return nil, false
	}
	return sessionKey.From(r.Context())
}

// MustFor returns the session or panics — for handlers whose routes carry the
// middleware, where its absence is a wiring bug.
func (m *Manager) MustFor(r *http.Request) *Session {
	s, ok := m.For(r)
	if !ok {
		panic("session: no session in context — is session.Middleware mounted on this route?")
	}
	return s
}

// fromContext returns the session for adapters that only have a context.
func fromContext(ctx context.Context) (*Session, bool) { return sessionKey.From(ctx) }

func withSession(ctx context.Context, s *Session) context.Context {
	// s.infoStore is embedded in the already-heap-allocated Session, so pointing
	// inf at it (instead of allocating a separate *Info) costs nothing extra.
	s.infoStore = Info{
		ID:         s.rec.ID,
		UserID:     s.rec.UserID,
		CreatedAt:  s.rec.CreatedAt,
		ExpiresAt:  s.rec.ExpiresAt,
		ElevatedAt: s.rec.ElevatedAt,
	}
	s.inf = &s.infoStore
	return infoKey.With(sessionKey.With(ctx, s), s.inf)
}

// TestWithSession stores s in ctx exactly as Middleware does. It exists for
// tests in other packages that need a session-bearing context without an HTTP
// server; production code should never call it.
func TestWithSession(ctx context.Context, s *Session) context.Context { return withSession(ctx, s) }

// LogExtractor adds a "session" group with the session id, and the user id when
// one is bound. Wire it with logger.WithContextExtractors(session.LogExtractor).
// It satisfies logger.ContextExtractor as a plain function, so it carries no
// package-level state.
func LogExtractor(ctx context.Context) (slog.Attr, bool) {
	inf, ok := infoKey.From(ctx)
	if !ok || inf.ID.IsZero() {
		return slog.Attr{}, false
	}
	attrs := []any{slog.String("id", inf.ID.String())}
	if inf.UserID != "" {
		attrs = append(attrs, slog.String("user", inf.UserID))
	}
	return slog.Group("session", attrs...), true
}
