package internal

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/dmitrymomot/forge/pkg/clientip"
	"github.com/dmitrymomot/forge/pkg/fingerprint"
	"github.com/dmitrymomot/forge/pkg/id"
	"github.com/dmitrymomot/forge/pkg/session"
	"github.com/dmitrymomot/forge/pkg/useragent"
)

// Default session configuration.
const (
	defaultSessionCookieName = "__sid"
	defaultSessionMaxAge     = 86400 * 30 // 30 days
)

// FingerprintMode determines which fingerprint generation algorithm to use.
type FingerprintMode int

const (
	// FingerprintDisabled disables fingerprint generation and validation.
	FingerprintDisabled FingerprintMode = iota
	// FingerprintCookie uses default settings, excludes IP. Best for most web apps.
	FingerprintCookie
	// FingerprintJWT uses minimal fingerprint (User-Agent + header set), excludes Accept headers.
	FingerprintJWT
	// FingerprintHTMX uses only User-Agent, avoids HTMX header variations.
	FingerprintHTMX
	// FingerprintStrict includes IP address. Use for high-security scenarios.
	// WARNING: Will cause false positives for mobile users, VPN users, and dynamic proxies.
	FingerprintStrict
)

// FingerprintStrictness determines behavior on fingerprint mismatch.
type FingerprintStrictness int

const (
	// FingerprintWarn logs a warning but allows the session to continue.
	// Use when you want visibility without disrupting users.
	FingerprintWarn FingerprintStrictness = iota
	// FingerprintReject invalidates the session on fingerprint mismatch.
	// Returns ErrFingerprintMismatch from LoadSession.
	FingerprintReject
)

// SessionManager handles session lifecycle and cookie management.
type SessionManager struct {
	store                 session.Store
	logger                *slog.Logger
	cookieName            string
	domain                string
	path                  string
	maxAge                int
	sameSite              http.SameSite
	fingerprintMode       FingerprintMode
	fingerprintStrictness FingerprintStrictness
	secure                bool
	httpOnly              bool
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

// WithSessionFingerprint enables device fingerprinting for session hijacking detection.
// Mode determines which components are included in the fingerprint:
//   - FingerprintCookie: Default, excludes IP (recommended for most apps)
//   - FingerprintJWT: Minimal, excludes Accept headers (for JWT apps)
//   - FingerprintHTMX: User-Agent only (for HTMX apps)
//   - FingerprintStrict: Includes IP (high-security, causes false positives)
//
// Strictness determines behavior on mismatch:
//   - FingerprintWarn: Log warning but allow session (visibility without disruption)
//   - FingerprintReject: Invalidate session (strict security)
func WithSessionFingerprint(mode FingerprintMode, strictness FingerprintStrictness) SessionOption {
	return func(sm *SessionManager) {
		sm.fingerprintMode = mode
		sm.fingerprintStrictness = strictness
	}
}

// SetLogger sets the logger for session events. Called by App after initialization.
func (sm *SessionManager) SetLogger(l *slog.Logger) {
	if l != nil {
		sm.logger = l
	}
}

// LoadSession loads an existing session from the request cookie.
// Returns nil, nil if no session cookie exists.
// Returns ErrNotFound if the session doesn't exist in the store.
// Returns ErrExpired if the session has expired.
// Returns ErrFingerprintMismatch if fingerprint validation fails (when strictness is FingerprintReject).
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

	// Validate fingerprint if enabled
	if sm.fingerprintMode != FingerprintDisabled && sess.Fingerprint != "" {
		if err := sm.validateFingerprint(r, sess); err != nil {
			if sm.fingerprintStrictness == FingerprintReject {
				return nil, session.ErrFingerprintMismatch
			}
			// FingerprintWarn: log and continue
			sm.logger.Warn("session fingerprint mismatch",
				slog.String("session_id", sess.ID),
				slog.String("ip", clientip.GetIP(r)),
				slog.String("user_agent", r.UserAgent()),
			)
		}
	}

	return sess, nil
}

// CreateSession creates a new session with metadata extracted from the request.
func (sm *SessionManager) CreateSession(ctx context.Context, r *http.Request) (*session.Session, error) {
	sessionID := id.NewULID()
	token, err := generateToken()
	if err != nil {
		return nil, fmt.Errorf("generate session token: %w", err)
	}
	expiresAt := time.Now().Add(time.Duration(sm.maxAge) * time.Second)

	sess := session.New(sessionID, token, expiresAt)
	sess.IP = clientip.GetIP(r)
	sess.UserAgent = r.UserAgent()
	sess.Device = parseDevice(r.UserAgent())
	sess.Fingerprint = sm.generateFingerprint(r)

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

// RotateToken generates a new token for the session.
// Called after authentication to prevent session fixation attacks by invalidating
// the old token and requiring a fresh one from the attacker.
func (sm *SessionManager) RotateToken(ctx context.Context, sess *session.Session) error {
	oldToken := sess.Token
	newToken, err := generateToken()
	if err != nil {
		return fmt.Errorf("generate session token: %w", err)
	}
	sess.Token = newToken
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
func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// parseDevice extracts device information from User-Agent using the useragent package.
// Returns a short identifier like "Chrome/128 (macOS, desktop)" or "Bot: Googlebot".
func parseDevice(ua string) string {
	if ua == "" {
		return "Unknown"
	}

	parsed, err := useragent.Parse(ua)
	if err != nil {
		return "Unknown"
	}

	return parsed.GetShortIdentifier()
}

// generateFingerprint creates a device fingerprint based on the configured mode.
func (sm *SessionManager) generateFingerprint(r *http.Request) string {
	switch sm.fingerprintMode {
	case FingerprintCookie:
		return fingerprint.Cookie(r)
	case FingerprintJWT:
		return fingerprint.JWT(r)
	case FingerprintHTMX:
		return fingerprint.HTMX(r)
	case FingerprintStrict:
		return fingerprint.Strict(r)
	default:
		return ""
	}
}

// validateFingerprint checks the stored fingerprint against the current request.
func (sm *SessionManager) validateFingerprint(r *http.Request, sess *session.Session) error {
	switch sm.fingerprintMode {
	case FingerprintCookie:
		return fingerprint.ValidateCookie(r, sess.Fingerprint)
	case FingerprintJWT:
		return fingerprint.ValidateJWT(r, sess.Fingerprint)
	case FingerprintHTMX:
		return fingerprint.ValidateHTMX(r, sess.Fingerprint)
	case FingerprintStrict:
		return fingerprint.ValidateStrict(r, sess.Fingerprint)
	default:
		return nil
	}
}
