package internal

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"strings"
	"time"

	"github.com/dmitrymomot/forge/pkg/session"
	"github.com/google/uuid"
)

// Default session configuration.
const (
	defaultSessionCookieName = "__sid"
	defaultSessionMaxAge     = 86400 * 30 // 30 days
)

// SessionManager handles session lifecycle and cookie management.
type SessionManager struct {
	store      session.Store
	cookieName string
	maxAge     int
	domain     string
	path       string
	secure     bool
	httpOnly   bool
	sameSite   http.SameSite
}

// SessionOption configures the SessionManager.
type SessionOption func(*SessionManager)

// NewSessionManager creates a new SessionManager with the given store and options.
func NewSessionManager(store session.Store, opts ...SessionOption) *SessionManager {
	sm := &SessionManager{
		store:      store,
		cookieName: defaultSessionCookieName,
		maxAge:     defaultSessionMaxAge,
		path:       "/",
		httpOnly:   true,
		sameSite:   http.SameSiteLaxMode,
	}

	for _, opt := range opts {
		opt(sm)
	}

	return sm
}

// WithSessionCookieName sets the session cookie name.
func WithSessionCookieName(name string) SessionOption {
	return func(sm *SessionManager) {
		if name != "" {
			sm.cookieName = name
		}
	}
}

// WithSessionMaxAge sets the session max age in seconds.
func WithSessionMaxAge(seconds int) SessionOption {
	return func(sm *SessionManager) {
		if seconds > 0 {
			sm.maxAge = seconds
		}
	}
}

// WithSessionDomain sets the session cookie domain.
func WithSessionDomain(domain string) SessionOption {
	return func(sm *SessionManager) {
		sm.domain = domain
	}
}

// WithSessionPath sets the session cookie path.
func WithSessionPath(path string) SessionOption {
	return func(sm *SessionManager) {
		if path != "" {
			sm.path = path
		}
	}
}

// WithSessionSecure sets the session cookie Secure flag.
func WithSessionSecure(secure bool) SessionOption {
	return func(sm *SessionManager) {
		sm.secure = secure
	}
}

// WithSessionHTTPOnly sets the session cookie HttpOnly flag.
func WithSessionHTTPOnly(httpOnly bool) SessionOption {
	return func(sm *SessionManager) {
		sm.httpOnly = httpOnly
	}
}

// WithSessionSameSite sets the session cookie SameSite attribute.
func WithSessionSameSite(sameSite http.SameSite) SessionOption {
	return func(sm *SessionManager) {
		sm.sameSite = sameSite
	}
}

// LoadSession loads an existing session from the request cookie.
// Returns nil, nil if no session cookie exists.
// Returns ErrNotFound if the session doesn't exist in the store.
// Returns ErrExpired if the session has expired.
func (sm *SessionManager) LoadSession(ctx context.Context, r *http.Request) (*session.Session, error) {
	cookie, err := r.Cookie(sm.cookieName)
	if err != nil {
		return nil, nil // No session cookie
	}

	token := cookie.Value
	if token == "" {
		return nil, nil
	}

	sess, err := sm.store.Get(ctx, token)
	if err != nil {
		return nil, err
	}

	return sess, nil
}

// CreateSession creates a new session with metadata extracted from the request.
func (sm *SessionManager) CreateSession(ctx context.Context, r *http.Request) (*session.Session, error) {
	id := uuid.New().String()
	token := generateToken()
	expiresAt := time.Now().Add(time.Duration(sm.maxAge) * time.Second)

	sess := session.New(id, token, expiresAt)
	sess.IP = extractIP(r)
	sess.UserAgent = r.UserAgent()
	sess.Device = parseDevice(r.UserAgent())

	if err := sm.store.Create(ctx, sess); err != nil {
		return nil, err
	}

	sess.ClearNew()
	sess.ClearDirty()

	return sess, nil
}

// SaveSession writes the session cookie to the response.
func (sm *SessionManager) SaveSession(w http.ResponseWriter, sess *session.Session) {
	cookie := &http.Cookie{
		Name:     sm.cookieName,
		Value:    sess.Token,
		Path:     sm.path,
		Domain:   sm.domain,
		MaxAge:   sm.maxAge,
		Secure:   sm.secure,
		HttpOnly: sm.httpOnly,
		SameSite: sm.sameSite,
	}
	http.SetCookie(w, cookie)
}

// RotateToken generates a new token for the session (for security after login).
// This prevents session fixation attacks.
func (sm *SessionManager) RotateToken(ctx context.Context, sess *session.Session) error {
	oldToken := sess.Token
	sess.Token = generateToken()
	sess.MarkDirty()

	// Update in store with new token
	if err := sm.store.Update(ctx, sess); err != nil {
		sess.Token = oldToken // Rollback on error
		return err
	}

	return nil
}

// DeleteSession clears the session cookie.
func (sm *SessionManager) DeleteSession(w http.ResponseWriter) {
	cookie := &http.Cookie{
		Name:     sm.cookieName,
		Value:    "",
		Path:     sm.path,
		Domain:   sm.domain,
		MaxAge:   -1,
		Secure:   sm.secure,
		HttpOnly: sm.httpOnly,
		SameSite: sm.sameSite,
	}
	http.SetCookie(w, cookie)
}

// Store returns the underlying session store.
func (sm *SessionManager) Store() session.Store {
	return sm.store
}

// generateToken creates a cryptographically secure random token.
func generateToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("session: failed to generate token: " + err.Error())
	}
	return base64.URLEncoding.EncodeToString(b)
}

// extractIP extracts the client IP from the request.
// Checks X-Forwarded-For and X-Real-IP headers first.
func extractIP(r *http.Request) string {
	// Check X-Forwarded-For (comma-separated, first is client)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			return strings.TrimSpace(parts[0])
		}
	}

	// Check X-Real-IP
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}

	// Fall back to RemoteAddr (strip port)
	addr := r.RemoteAddr
	if idx := strings.LastIndex(addr, ":"); idx != -1 {
		return addr[:idx]
	}
	return addr
}

// parseDevice extracts a simple device description from User-Agent.
// Returns something like "Chrome on macOS" or "Safari on iOS".
func parseDevice(ua string) string {
	if ua == "" {
		return "Unknown"
	}

	// Simple browser detection
	var browser string
	switch {
	case strings.Contains(ua, "Firefox"):
		browser = "Firefox"
	case strings.Contains(ua, "Edg"):
		browser = "Edge"
	case strings.Contains(ua, "Chrome"):
		browser = "Chrome"
	case strings.Contains(ua, "Safari"):
		browser = "Safari"
	case strings.Contains(ua, "Opera") || strings.Contains(ua, "OPR"):
		browser = "Opera"
	default:
		browser = "Unknown Browser"
	}

	// Simple OS detection
	var os string
	switch {
	case strings.Contains(ua, "iPhone") || strings.Contains(ua, "iPad"):
		os = "iOS"
	case strings.Contains(ua, "Android"):
		os = "Android"
	case strings.Contains(ua, "Mac OS"):
		os = "macOS"
	case strings.Contains(ua, "Windows"):
		os = "Windows"
	case strings.Contains(ua, "Linux"):
		os = "Linux"
	default:
		os = "Unknown OS"
	}

	return browser + " on " + os
}
