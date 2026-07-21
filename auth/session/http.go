package session

import (
	"context"
	"net/http"
	"time"
	"unicode/utf8"

	"github.com/dmitrymomot/forge/web/clientip"
)

// maxUserAgentLen caps the stored User-Agent so an attacker-controlled header
// can't bloat records (or blow the cookiestore size limit).
const maxUserAgentLen = 256

// Transport carries the bearer token between client and server — the seam
// the request-level Manager methods ride. Implementations ship in
// session/transport (Cookie, Bearer, Basic, JWT); anything satisfying the
// interface plugs in via WithTransport.
type Transport interface {
	// Extract returns the token presented by r, or "" when none is carried.
	Extract(r *http.Request) string
	// Embed hands token to the client on w (Set-Cookie, response header).
	// expiresAt bounds client-side retention. Transports whose credential is
	// client-managed (Basic) may no-op.
	Embed(w http.ResponseWriter, token string, expiresAt time.Time) error
	// Clear removes the client-side credential.
	Clear(w http.ResponseWriter) error
}

// ClientInfo extracts display metadata (client IP, User-Agent) from a
// request for device listings. See WithClientInfo.
type ClientInfo func(r *http.Request) (ip, userAgent string)

func defaultClientInfo(r *http.Request) (string, string) {
	return clientip.Resolve(r), r.UserAgent()
}

// LoadRequest extracts the token via the configured Transport and loads its
// session — Load's request-level form. A request carrying no token fails
// with ErrNotFound.
func (m *Manager[T]) LoadRequest(r *http.Request) (*Session[T], error) {
	t, err := m.transport()
	if err != nil {
		return nil, err
	}
	return m.Load(r.Context(), t.Extract(r))
}

// SaveRequest stamps the session's client metadata (IP, User-Agent),
// persists it, and embeds the current token in the response — Save's
// request-level form. Call it whenever the session changed; the transport
// keeps the client's credential in sync (stateless stores mint a new token
// on every save).
func (m *Manager[T]) SaveRequest(w http.ResponseWriter, r *http.Request, s *Session[T]) error {
	return m.saveRequest(w, r, s, (*Manager[T]).Save)
}

// AuthenticateRequest is Authenticate's request-level form: it binds userID,
// rotates the token, and embeds the replacement in the response.
func (m *Manager[T]) AuthenticateRequest(w http.ResponseWriter, r *http.Request, s *Session[T], userID string) error {
	return m.saveRequest(w, r, s, func(m *Manager[T], ctx context.Context, s *Session[T]) error {
		return m.Authenticate(ctx, s, userID)
	})
}

// RotateRequest is Rotate's request-level form: it re-keys the session and
// embeds the replacement token in the response.
func (m *Manager[T]) RotateRequest(w http.ResponseWriter, r *http.Request, s *Session[T]) error {
	return m.saveRequest(w, r, s, (*Manager[T]).Rotate)
}

// DestroyRequest is Destroy's request-level form: it revokes the session and
// clears the client-side credential.
func (m *Manager[T]) DestroyRequest(w http.ResponseWriter, r *http.Request, s *Session[T]) error {
	t, err := m.transport()
	if err != nil {
		return err
	}
	if err := m.Destroy(r.Context(), s); err != nil {
		return err
	}
	return t.Clear(w)
}

func (m *Manager[T]) transport() (Transport, error) {
	if m.cfg.transport == nil {
		return nil, ErrNoTransport
	}
	return m.cfg.transport, nil
}

func (m *Manager[T]) saveRequest(w http.ResponseWriter, r *http.Request, s *Session[T], op func(*Manager[T], context.Context, *Session[T]) error) error {
	t, err := m.transport()
	if err != nil {
		return err
	}
	ip, ua := m.cfg.clientInfo(r)
	if len(ua) > maxUserAgentLen {
		// Cut on a rune boundary: a byte-count slice through a multi-byte
		// UTF-8 sequence would store an invalid string.
		cut := maxUserAgentLen
		for cut > 0 && !utf8.RuneStart(ua[cut]) {
			cut--
		}
		ua = ua[:cut]
	}
	s.IP, s.UserAgent = ip, ua
	if err := op(m, r.Context(), s); err != nil {
		return err
	}
	return t.Embed(w, s.Token, s.ExpiresAt)
}
