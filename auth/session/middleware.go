package session

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/dmitrymomot/forge/web/middleware"
	"github.com/dmitrymomot/forge/web/problem"
)

type mwOptions struct {
	responder  problem.Responder
	logger     *slog.Logger
	clientInfo func(*http.Request) Bind
	transports []Transport
	policies   []Policy
}

// MiddlewareOption configures Middleware.
type MiddlewareOption func(*mwOptions)

// WithTransport registers transports. Extraction tries them in order;
// embedding uses whichever matched, or the first that supports it.
func WithTransport(ts ...Transport) MiddlewareOption {
	return func(o *mwOptions) { o.transports = append(o.transports, ts...) }
}

// WithPolicy registers request-time policies, run in order, short-circuiting.
func WithPolicy(ps ...Policy) MiddlewareOption {
	return func(o *mwOptions) { o.policies = append(o.policies, ps...) }
}

// WithResponder overrides the rejection response. Default: problem.JSON 401.
func WithResponder(r problem.Responder) MiddlewareOption {
	return func(o *mwOptions) { o.responder = r }
}

// WithMiddlewareLogger sets the middleware's logger.
func WithMiddlewareLogger(l *slog.Logger) MiddlewareOption {
	return func(o *mwOptions) { o.logger = l }
}

// WithClientInfo supplies the device metadata pinned when a session is first
// created. Session does not extract client IPs or compute fingerprints —
// web/clientip and web/fingerprint do — so it takes a function.
//
// It runs only for a session that has never been persisted. Refreshing these
// columns per request would not weaken binding, it would delete it: a stolen
// credential's first request would rewrite the row with the attacker's address
// and fingerprint, and every later request would match. Use Manager.Rebind for
// the deliberate re-pin after a successful re-authentication.
func WithClientInfo(fn func(*http.Request) Bind) MiddlewareOption {
	return func(o *mwOptions) { o.clientInfo = fn }
}

// Middleware loads the session, runs policies, exposes it on the context, and
// commits exactly once before the first byte of the response.
//
// A visitor with no credential gets a fresh anonymous session and costs no
// storage: the row is minted only if the handler writes something. An unknown
// or expired credential is treated the same way. An infrastructure failure is
// never degraded to an anonymous request.
func Middleware(m *Manager, opts ...MiddlewareOption) middleware.Middleware {
	o := mwOptions{
		responder: problem.JSON(problem.WithStatus(http.StatusUnauthorized)),
		logger:    m.log,
	}
	for _, opt := range opts {
		opt(&o)
	}
	if len(o.transports) == 0 {
		panic("session: Middleware requires at least one WithTransport")
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sess, matchedIdx, err := load(m, &o, r)
			if err != nil {
				o.logger.ErrorContext(r.Context(), "session: load failed", slog.Any("error", err))
				problem.JSON(problem.WithStatus(http.StatusInternalServerError))(w, r, err)
				return
			}
			if o.clientInfo != nil && sess.isNew {
				b := o.clientInfo(r)
				sess.rec.IP, sess.rec.UserAgent, sess.rec.Fingerprint = b.IP, b.UserAgent, b.Fingerprint
			}

			// A new (never-persisted) session means the client holds no credential
			// yet, so the presented token is "". A loaded session's token IS what
			// the client presented — remembered here because Authenticate/Rotate
			// mutate s.token in place before commit ever runs.
			presentedToken := ""
			if !sess.isNew {
				presentedToken = sess.token
			}

			ctx := withSession(r.Context(), sess)
			r = r.WithContext(ctx)

			if err := runPolicies(m, &o, w, r, sess, matchedIdx); err != nil {
				return // runPolicies already answered
			}

			cw := newCommitWriter(w, func() error { return commit(m, &o, w, r, sess, matchedIdx, presentedToken) })
			next.ServeHTTP(cw, r)

			if !cw.committed {
				if err := cw.ensureCommitted(); err != nil {
					// The handler wrote nothing, so no status has gone out yet.
					// Mirror commitWriter.WriteHeader's failure branch: a failed
					// commit must be a clean 500, not an implicit 200 that hides
					// a store failure and a never-persisted credential.
					o.logger.ErrorContext(ctx, "session: commit failed after handler", slog.Any("error", err))
					clearHeaders(w.Header())
					w.WriteHeader(http.StatusInternalServerError)
				}
			}
		})
	}
}

// load resolves the session for r, returning the index into o.transports of
// the transport that matched, or -1 when none did.
func load(m *Manager, o *mwOptions, r *http.Request) (*Session, int, error) {
	for i, t := range o.transports {
		token, ok := t.Extract(r)
		if !ok {
			continue
		}
		sess, err := m.Load(r.Context(), token)
		switch {
		case err == nil:
			return sess, i, nil
		case errors.Is(err, ErrNotFound), errors.Is(err, ErrExpired):
			// A credential we cannot honor is anonymous, not an error.
			return m.Start(), i, nil
		default:
			return nil, -1, err
		}
	}
	return m.Start(), -1, nil
}

func runPolicies(m *Manager, o *mwOptions, w http.ResponseWriter, r *http.Request, s *Session, matchedIdx int) error {
	for _, p := range o.policies {
		err := p(r.Context(), r, s)
		if err == nil {
			continue
		}
		reason, revoke := IsRevoke(err)
		if revoke {
			if delErr := m.Destroy(r.Context(), s); delErr != nil {
				o.logger.ErrorContext(r.Context(), "session: revoke could not delete the record",
					slog.String("reason", reason), slog.Any("error", delErr))
			}
			clearCredential(o, w, r, matchedIdx)
			o.logger.InfoContext(r.Context(), "session: revoked", slog.String("reason", reason))
			o.responder(w, r, err)
			return err
		}
		if reason, deny := IsDeny(err); deny {
			o.logger.InfoContext(r.Context(), "session: denied", slog.String("reason", reason))
			o.responder(w, r, err)
			return err
		}
		// Anything else is infrastructure: fail closed.
		o.logger.ErrorContext(r.Context(), "session: policy failed", slog.Any("error", err))
		problem.JSON(problem.WithStatus(http.StatusInternalServerError))(w, r, err)
		return err
	}
	return nil
}

// commit persists the session and writes the credential. It runs at most once,
// from the commit writer, while the response headers are still open.
//
// presentedToken is the credential the client actually presented on this
// request ("" for a brand-new session). A handler that calls Authenticate or
// Rotate mid-request already saved the session and minted a fresh token by
// the time commit runs, so the session is neither dirty nor new — without
// this check the rotated credential would never reach the client.
func commit(m *Manager, o *mwOptions, w http.ResponseWriter, r *http.Request, s *Session, matchedIdx int, presentedToken string) error {
	if s.deleted {
		clearCredential(o, w, r, matchedIdx)
		return nil
	}
	// A dirty payload, or a new session that carries data worth persisting, needs a save.
	if s.isDirty() || (s.isNew && s.Authenticated()) {
		if err := m.Save(r.Context(), s); err != nil {
			return err
		}
		return embed(o, w, r, s, matchedIdx)
	}
	// Authenticate/Rotate already persisted a fresh credential the client does not yet hold: embed it.
	if !s.isNew && s.token != presentedToken {
		return embed(o, w, r, s, matchedIdx)
	}
	// Otherwise a metadata-only refresh, fail-open: a failed touch must not fail the request.
	if m.touchDue(s, m.now()) {
		if err := m.toucher.Touch(r.Context(), s.token, s.rec.LastSeenAt, s.rec.ExpiresAt); err != nil {
			o.logger.WarnContext(r.Context(), "session: touch failed", slog.Any("error", err))
		}
	}
	return nil
}

// embed writes the credential using the matched transport, falling through to
// the first transport that supports embedding when the matched one cannot.
// matchedIdx is the index into o.transports of the matched transport, or -1
// when none matched; indices are compared instead of Transport values because
// a transport's dynamic type is not guaranteed to be comparable.
func embed(o *mwOptions, w http.ResponseWriter, r *http.Request, s *Session, matchedIdx int) error {
	if matchedIdx >= 0 {
		err := o.transports[matchedIdx].Embed(w, r, s)
		if err == nil {
			return nil
		}
		if !errors.Is(err, ErrNoEmbed) {
			return err
		}
	}
	for i, t := range o.transports {
		if i == matchedIdx {
			continue
		}
		err := t.Embed(w, r, s)
		if errors.Is(err, ErrNoEmbed) {
			continue
		}
		return err
	}
	return ErrNoEmbed
}

func clearCredential(o *mwOptions, w http.ResponseWriter, r *http.Request, matchedIdx int) {
	if matchedIdx >= 0 {
		o.transports[matchedIdx].Clear(w, r)
		return
	}
	for _, t := range o.transports {
		t.Clear(w, r)
	}
}
