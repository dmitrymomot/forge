package internal

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/dmitrymomot/forge/pkg/clientip"
	"github.com/dmitrymomot/forge/pkg/fingerprint"
	"github.com/dmitrymomot/forge/pkg/id"
	"github.com/dmitrymomot/forge/pkg/useragent"
)

var (
	// ErrSessionNotConfigured is returned when session functionality is used
	// but WithSession was not configured on the app.
	ErrSessionNotConfigured = errors.New("session: not configured")

	// ErrSessionNotFound is returned when a session does not exist.
	ErrSessionNotFound = errors.New("session: not found")

	// ErrSessionExpired is returned when a session has expired.
	ErrSessionExpired = errors.New("session: expired")

	// ErrSessionInvalidToken is returned when a session token is invalid.
	ErrSessionInvalidToken = errors.New("session: invalid token")

	// ErrSessionFingerprintMismatch is returned when session fingerprint validation fails.
	// This may indicate a session hijacking attempt.
	ErrSessionFingerprintMismatch = errors.New("session: fingerprint mismatch")
)

// FingerprintMode determines which fingerprint generation algorithm to use.
//
// Fingerprinting helps detect session hijacking by validating that requests
// come from the same device that created the session.
//
// Example configuration:
//
//	app := forge.New(
//	    forge.WithSession(store,
//	        forge.WithSessionFingerprint(
//	            forge.FingerprintCookie,  // Mode
//	            forge.FingerprintWarn,    // Strictness
//	        ),
//	    ),
//	)
type FingerprintMode int

const (
	// FingerprintDisabled disables fingerprint generation and validation.
	// Use this for maximum compatibility.
	FingerprintDisabled FingerprintMode = iota

	// FingerprintCookie uses default settings, excludes IP. Best for most web apps.
	// Validates User-Agent and common request headers.
	FingerprintCookie

	// FingerprintJWT uses minimal fingerprint (User-Agent + header set), excludes Accept headers.
	// Optimized for API clients that may send varying Accept headers.
	FingerprintJWT

	// FingerprintHTMX uses only User-Agent, avoids HTMX header variations.
	// Use this for HTMX-heavy applications where HX-* headers change frequently.
	FingerprintHTMX

	// FingerprintStrict includes IP address. Use for high-security scenarios.
	// WARNING: Will cause false positives for mobile users, VPN users, and dynamic proxies.
	// Only use if your users are on stable networks (e.g., corporate intranet).
	FingerprintStrict
)

// FingerprintStrictness determines behavior on fingerprint mismatch.
type FingerprintStrictness int

const (
	// FingerprintWarn logs a warning but allows the session to continue.
	// Use when you want visibility without disrupting users.
	FingerprintWarn FingerprintStrictness = iota
	// FingerprintReject invalidates the session on fingerprint mismatch.
	// Returns ErrSessionFingerprintMismatch from LoadSession.
	FingerprintReject
)

// Session represents a user session with metadata and arbitrary values.
//
// # Goroutine Safety
//
// Session methods are NOT goroutine-safe. Session instances should only be accessed
// from a single request context. If you need to share session data across goroutines,
// make a copy of the required values first.
//
// # Auto-Creation
//
// Sessions are automatically created when accessed via Context methods:
//   - c.Session() - loads existing or creates anonymous session
//   - c.SessionValue(key) - auto-creates if needed, returns value
//   - c.SetSessionValue(key, val) - auto-creates if needed, stores value
//
// No manual c.InitSession() is required. Sessions are lazily created on first access.
//
// # Session Lifecycle
//
// Anonymous sessions are created with UserID = nil. When a user authenticates,
// call c.AuthenticateSession(userID) to promote the session and rotate the token
// for session fixation prevention.
//
// Example:
//
//	func LoginHandler(c forge.Context) error {
//	    // Validate credentials...
//	    if err := c.AuthenticateSession(user.ID); err != nil {
//	        return err
//	    }
//	    return c.Redirect(http.StatusSeeOther, "/dashboard")
//	}
//
// # Session Limits
//
// By default, users can have up to 5 concurrent sessions (configurable up to 10).
// When the limit is reached, the oldest session is automatically deleted.
// This prevents session accumulation and provides "logout from all devices" functionality.
type Session struct {
	CreatedAt    time.Time
	LastActiveAt time.Time
	ExpiresAt    time.Time

	UserID      *string        // nil = anonymous session
	Data        map[string]any // Arbitrary session data
	ID          string         // Unique identifier (ULID)
	TokenHash   string         // SHA-256 hash of the cookie token
	IP          string         // Client IP address
	UserAgent   string         // Raw User-Agent header
	Device      string         // Parsed device info (e.g., "Chrome/128 (macOS, desktop)")
	Fingerprint string         // Device fingerprint for hijacking detection

	dirty bool
	isNew bool
}

// Store defines the interface for session persistence.
//
// # Database Requirements
//
// Your Store implementation MUST create the following indexes for performance:
//
//	CREATE INDEX idx_sessions_token_hash ON sessions(token_hash);
//	CREATE INDEX idx_sessions_user_id ON sessions(user_id);
//	CREATE INDEX idx_sessions_expires_at ON sessions(expires_at);
//
// The token_hash index is CRITICAL for session lookup performance.
//
// # Data Serialization
//
// The Store is responsible for serializing the Session.Data map[string]any field.
// For SQL databases, use JSONB or JSON column type. Example Postgres schema below.
//
// # Example Postgres Schema
//
//	CREATE TABLE sessions (
//	    id           TEXT PRIMARY KEY,
//	    token_hash   TEXT NOT NULL,
//	    user_id      TEXT,
//	    data         JSONB NOT NULL DEFAULT '{}',
//	    ip           TEXT NOT NULL DEFAULT '',
//	    user_agent   TEXT NOT NULL DEFAULT '',
//	    device       TEXT NOT NULL DEFAULT '',
//	    fingerprint  TEXT NOT NULL DEFAULT '',
//	    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
//	    last_active_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
//	    expires_at   TIMESTAMPTZ NOT NULL
//	);
//
//	CREATE INDEX idx_sessions_token_hash ON sessions(token_hash);
//	CREATE INDEX idx_sessions_user_id ON sessions(user_id) WHERE user_id IS NOT NULL;
//	CREATE INDEX idx_sessions_expires_at ON sessions(expires_at);
//
// # Cleanup Job
//
// Implement a background job to delete expired sessions:
//
//	DELETE FROM sessions WHERE expires_at < NOW();
//
// Run this hourly or daily depending on your traffic.
type Store interface {
	// Create persists a new session.
	// The Store is responsible for serializing Session.Data to JSON.
	Create(ctx context.Context, s *Session) error

	// GetByTokenHash retrieves a session by its SHA-256 token hash.
	// Returns ErrSessionNotFound if the session doesn't exist.
	// Returns ErrSessionExpired if the session has expired.
	//
	// PERFORMANCE: This method is called on EVERY request with a session cookie.
	// It MUST use an index on token_hash column for fast lookups.
	//
	// The Store is responsible for deserializing Session.Data from JSON.
	GetByTokenHash(ctx context.Context, tokenHash string) (*Session, error)

	// Update saves changes to an existing session.
	// The Store is responsible for serializing Session.Data to JSON.
	Update(ctx context.Context, s *Session) error

	// Delete removes a session by its ID.
	Delete(ctx context.Context, id string) error

	// ListByUserID retrieves all sessions for a user.
	// Used for "view active sessions" functionality.
	// The Store is responsible for deserializing Session.Data from JSON.
	ListByUserID(ctx context.Context, userID string) ([]*Session, error)

	// CountByUserID returns the number of sessions for a user.
	// Used for enforcing session limits.
	CountByUserID(ctx context.Context, userID string) (int, error)

	// DeleteByUserID removes all sessions for a user.
	// Useful for "logout from all devices" functionality.
	DeleteByUserID(ctx context.Context, userID string) error

	// DeleteByUserIDExcept removes all sessions for a user except the specified session ID.
	// Used for "logout from other devices" functionality.
	// FIX #6: Enables batch delete instead of N+1 queries.
	DeleteByUserIDExcept(ctx context.Context, userID, exceptID string) error

	// DeleteOldestByUserID removes the oldest session for a user (by last_active_at).
	// Used for enforcing max session limits per user.
	DeleteOldestByUserID(ctx context.Context, userID string) error

	// Touch updates the LastActiveAt timestamp without loading the full session.
	// Used for activity tracking without full session updates when touch threshold is met.
	Touch(ctx context.Context, id string, lastActiveAt time.Time) error
}

// NewSession creates a new session with the given token and expiration.
func NewSession(token string, expiresAt time.Time) *Session {
	now := time.Now()
	return &Session{
		ID:           id.NewULID(),
		TokenHash:    hashToken(token),
		Data:         make(map[string]any),
		CreatedAt:    now,
		LastActiveAt: now,
		ExpiresAt:    expiresAt,
		isNew:        true,
		dirty:        true,
	}
}

// generateToken creates a cryptographically secure random token.
// Returns 43-character base64url string from 32 random bytes.
func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// hashToken returns the SHA-256 hash of the token for storage.
//
// # Why SHA-256 (not bcrypt)?
//
// Session tokens are 32 bytes from crypto/rand (256 bits of entropy).
// This makes them computationally impossible to brute-force (2^256 combinations).
//
// SHA-256 is used because:
//   - Fast lookups via database index on token_hash column
//   - Deterministic (same input = same output, required for DB queries)
//   - Secure for high-entropy tokens (tokens cannot be guessed)
//
// bcrypt is NOT used because:
//   - Non-deterministic (same input = different output each time due to salt)
//   - Cannot be used for database lookups (WHERE token_hash = ? would never match)
//   - Only needed for low-entropy secrets like passwords
//
// For passwords, use bcrypt. For random tokens, use SHA-256.
func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return base64.URLEncoding.EncodeToString(h[:])
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

// SetValue stores a value in the session.
// Marks the session as dirty for automatic saving.
//
// WARNING: NOT goroutine-safe. Only call from the request context.
func (s *Session) SetValue(key string, val any) {
	if s.Data == nil {
		s.Data = make(map[string]any)
	}
	s.Data[key] = val
	s.dirty = true
}

// GetValue retrieves a value from the session.
// Returns (value, true) if found, (nil, false) if not found.
//
// WARNING: NOT goroutine-safe. Only call from the request context.
func (s *Session) GetValue(key string) (any, bool) {
	if s.Data == nil {
		return nil, false
	}
	val, ok := s.Data[key]
	return val, ok
}

// DeleteValue removes a value from the session.
// Marks the session as dirty only if the key existed.
//
// WARNING: NOT goroutine-safe. Only call from the request context.
func (s *Session) DeleteValue(key string) {
	if s.Data == nil {
		return
	}
	if _, exists := s.Data[key]; exists {
		delete(s.Data, key)
		s.dirty = true
	}
}

// IsDirty returns true if the session has unsaved changes.
func (s *Session) IsDirty() bool {
	return s.dirty
}

// ClearDirty marks the session as clean (saved).
// Called by the session manager after persisting changes.
func (s *Session) ClearDirty() {
	s.dirty = false
}

// MarkDirty marks the session as needing to be saved.
func (s *Session) MarkDirty() {
	s.dirty = true
}

func (s *Session) IsNew() bool {
	return s.isNew
}

func (s *Session) ClearNew() {
	s.isNew = false
}

func (s *Session) IsExpired() bool {
	return time.Now().After(s.ExpiresAt)
}

func (s *Session) IsAuthenticated() bool {
	return s.UserID != nil && *s.UserID != ""
}

const (
	defaultSessionTTL         = 30 * 24 * time.Hour // 30 days
	defaultMaxSessionsPerUser = 5
	defaultTouchThreshold     = 5 * time.Minute
	defaultCookieName         = "__sid"
	maxAllowedSessionsPerUser = 10
)

// sessionConfig holds all session configuration.
// This is configured via App options and shared with sessionManager.
type sessionConfig struct {
	logger                *slog.Logger
	cookieName            string
	cookiePath            string
	cookieDomain          string
	ttl                   time.Duration
	touchThreshold        time.Duration
	cookieSameSite        http.SameSite
	fingerprintMode       FingerprintMode
	fingerprintStrictness FingerprintStrictness
	maxSessionsPerUser    int
	cookieSecure          bool
	cookieHTTPOnly        bool
}

// sessionManager is an unexported helper for session operations.
// It's created by requestContext from App's sessionStore and sessionConfig.
//
// # Security Features
//
// Token Hashing: Session tokens are hashed with SHA-256 before storage.
// Only the hash is stored in the database, not the plaintext token.
// This prevents token theft if the database is compromised.
//
// Token Rotation: Tokens are rotated on authentication to prevent session fixation attacks.
//
// Fingerprint Validation: Optional device fingerprinting to detect session hijacking.
// Configure with WithSessionFingerprint(mode, strictness).
//
// Session Limits: Enforces maximum concurrent sessions per user (default 5, max 10).
// Oldest sessions are automatically deleted when the limit is reached.
//
// # Performance Optimization
//
// Touch Threshold: Session LastActiveAt is only updated if the threshold has been exceeded
// (default 5 minutes). This reduces database writes for high-traffic sessions while maintaining
// reasonable activity tracking.
//
// Example: If touchThreshold is 5 minutes, and a user makes 100 requests in 1 minute,
// only the first request will update LastActiveAt. The other 99 skip the database write.
type sessionManager struct {
	store  Store
	config *sessionConfig
}

// SessionOption configures session settings.
type SessionOption func(*sessionConfig)

// WithSessionTTL sets the session time-to-live duration.
func WithSessionTTL(ttl time.Duration) SessionOption {
	return func(cfg *sessionConfig) {
		cfg.ttl = ttl
	}
}

// WithMaxSessionsPerUser sets the maximum concurrent sessions per user.
// Value is capped at maxAllowedSessionsPerUser (10).
func WithMaxSessionsPerUser(max int) SessionOption {
	return func(cfg *sessionConfig) {
		if max > maxAllowedSessionsPerUser {
			max = maxAllowedSessionsPerUser
		}
		cfg.maxSessionsPerUser = max
	}
}

// WithSessionTouchThreshold sets the minimum time between LastActiveAt updates.
func WithSessionTouchThreshold(threshold time.Duration) SessionOption {
	return func(cfg *sessionConfig) {
		cfg.touchThreshold = threshold
	}
}

// WithSessionCookieName sets the session cookie name.
func WithSessionCookieName(name string) SessionOption {
	return func(cfg *sessionConfig) {
		cfg.cookieName = name
	}
}

// WithSessionCookieDomain sets the session cookie domain.
func WithSessionCookieDomain(domain string) SessionOption {
	return func(cfg *sessionConfig) {
		cfg.cookieDomain = domain
	}
}

// WithSessionCookiePath sets the session cookie path.
func WithSessionCookiePath(path string) SessionOption {
	return func(cfg *sessionConfig) {
		cfg.cookiePath = path
	}
}

// WithSessionCookieSecure sets the session cookie secure flag.
func WithSessionCookieSecure(secure bool) SessionOption {
	return func(cfg *sessionConfig) {
		cfg.cookieSecure = secure
	}
}

// WithSessionCookieHTTPOnly sets the session cookie httpOnly flag.
func WithSessionCookieHTTPOnly(httpOnly bool) SessionOption {
	return func(cfg *sessionConfig) {
		cfg.cookieHTTPOnly = httpOnly
	}
}

// WithSessionCookieSameSite sets the session cookie sameSite attribute.
func WithSessionCookieSameSite(sameSite http.SameSite) SessionOption {
	return func(cfg *sessionConfig) {
		cfg.cookieSameSite = sameSite
	}
}

// WithSessionFingerprint configures fingerprint validation.
func WithSessionFingerprint(mode FingerprintMode, strictness FingerprintStrictness) SessionOption {
	return func(cfg *sessionConfig) {
		cfg.fingerprintMode = mode
		cfg.fingerprintStrictness = strictness
	}
}

// WithSessionLogger sets the logger for session events.
func WithSessionLogger(logger *slog.Logger) SessionOption {
	return func(cfg *sessionConfig) {
		cfg.logger = logger
	}
}

// Unexported sessionManager methods - take Context interface instead of separate parameters

// loadSession loads a session from the request cookie via Context interface.
// Returns ErrSessionNotFound if no session cookie exists or session doesn't exist in store.
// Returns ErrSessionExpired if the session has expired.
// Returns ErrSessionFingerprintMismatch if fingerprint validation fails (when strictness is FingerprintReject).
func (sm *sessionManager) loadSession(c Context) (*Session, error) {
	token := sm.getTokenFromCookie(c.Request())
	if token == "" {
		return nil, ErrSessionNotFound
	}

	tokenHash := hashToken(token)
	sess, err := sm.store.GetByTokenHash(c.Context(), tokenHash)
	if err != nil {
		return nil, err
	}

	// Check expiration
	if time.Now().After(sess.ExpiresAt) {
		_ = sm.store.Delete(c.Context(), sess.ID) // Best-effort cleanup
		return nil, ErrSessionExpired
	}

	// Validate fingerprint
	if err := sm.validateFingerprint(c, sess); err != nil {
		if sm.config.fingerprintStrictness == FingerprintReject {
			_ = sm.store.Delete(c.Context(), sess.ID) // Best-effort cleanup
			return nil, err
		}
		// Log warning but continue
		if sm.config.logger != nil {
			sm.config.logger.WarnContext(c.Context(), "session fingerprint mismatch",
				"session_id", sess.ID, "error", err)
		}
	}

	return sess, nil
}

// createSession creates a new anonymous session using Context interface.
// Returns the session and the plain token for cookie storage.
func (sm *sessionManager) createSession(c Context) (*Session, string, error) {
	token, err := generateToken()
	if err != nil {
		return nil, "", err
	}

	fp := sm.generateFingerprint(c)
	sess := &Session{
		ID:           id.NewULID(),
		TokenHash:    hashToken(token),
		CreatedAt:    time.Now(),
		LastActiveAt: time.Now(),
		ExpiresAt:    time.Now().Add(sm.config.ttl),
		Fingerprint:  fp,
		IP:           clientip.GetIP(c.Request()),
		UserAgent:    c.Request().UserAgent(),
		Device:       parseDevice(c.Request().UserAgent()),
		Data:         make(map[string]any),
	}

	if err := sm.store.Create(c.Context(), sess); err != nil {
		return nil, "", err
	}

	return sess, token, nil
}

// updateSession persists session changes to the store.
// FIX #1: Always use Update when session is dirty - removed buggy touchOnly optimization.
func (sm *sessionManager) updateSession(c Context, sess *Session) error {
	if !sess.IsDirty() {
		return nil
	}

	// Always use full Update when dirty to avoid data loss
	sess.LastActiveAt = time.Now()
	if err := sm.store.Update(c.Context(), sess); err != nil {
		return err
	}

	sess.ClearDirty()
	return nil
}

// authenticateSession promotes an anonymous session to an authenticated one.
// FIX #5: Single update after all changes instead of split writes.
// Always rotates tokens on authentication to prevent session fixation attacks.
func (sm *sessionManager) authenticateSession(c Context, sess *Session, userID string) (string, error) {
	sess.UserID = &userID
	sess.MarkDirty()

	// Always rotate token on authentication (session fixation prevention)
	newToken, err := sm.rotateToken(c.Context(), sess)
	if err != nil {
		return "", err
	}

	// Enforce session limit
	if err := sm.enforceSessionLimit(c.Context(), userID); err != nil {
		return "", err
	}

	// FIX #5: Single update after all changes
	if err := sm.updateSession(c, sess); err != nil {
		return "", err
	}

	return newToken, nil
}

// rotateToken generates a new token for the session without persisting.
// FIX #5: Don't persist here - let caller handle it.
func (sm *sessionManager) rotateToken(ctx context.Context, sess *Session) (string, error) {
	newToken, err := generateToken()
	if err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}

	sess.TokenHash = hashToken(newToken)
	sess.MarkDirty()

	return newToken, nil
}

// saveSessionCookie writes the session cookie using Context.SetCookie.
// FIX #4: Use Context.SetCookie instead of building http.Cookie.
func (sm *sessionManager) saveSessionCookie(c Context, token string) {
	maxAge := int(sm.config.ttl.Seconds())
	c.SetCookie(sm.config.cookieName, token, maxAge)
}

// deleteSessionCookie removes the session cookie using Context.SetCookie.
// FIX #4: Use Context.SetCookie with negative maxAge.
func (sm *sessionManager) deleteSessionCookie(c Context) {
	c.SetCookie(sm.config.cookieName, "", -1)
}

// destroySession removes a session from the store.
func (sm *sessionManager) destroySession(ctx context.Context, sessionID string) error {
	return sm.store.Delete(ctx, sessionID)
}

// destroyOtherSessions removes all sessions for a user except the specified one.
// FIX #6: Use batch delete via DeleteByUserIDExcept.
func (sm *sessionManager) destroyOtherSessions(ctx context.Context, userID, exceptID string) error {
	return sm.store.DeleteByUserIDExcept(ctx, userID, exceptID)
}

// destroyAllUserSessions removes all sessions for a user.
func (sm *sessionManager) destroyAllUserSessions(ctx context.Context, userID string) error {
	return sm.store.DeleteByUserID(ctx, userID)
}

// listUserSessions retrieves all sessions for a user.
func (sm *sessionManager) listUserSessions(ctx context.Context, userID string) ([]*Session, error) {
	return sm.store.ListByUserID(ctx, userID)
}

// Private helpers

// getTokenFromCookie extracts the session token from the cookie.
func (sm *sessionManager) getTokenFromCookie(r *http.Request) string {
	cookie, err := r.Cookie(sm.config.cookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}

// validateFingerprint checks the stored fingerprint against the current request.
func (sm *sessionManager) validateFingerprint(c Context, sess *Session) error {
	if sm.config.fingerprintMode == FingerprintDisabled || sess.Fingerprint == "" {
		return nil
	}

	currentFP := sm.generateFingerprint(c)
	if currentFP != sess.Fingerprint {
		return ErrSessionFingerprintMismatch
	}

	return nil
}

// generateFingerprint creates a device fingerprint based on the configured mode.
func (sm *sessionManager) generateFingerprint(c Context) string {
	r := c.Request()
	switch sm.config.fingerprintMode {
	case FingerprintDisabled:
		return ""
	case FingerprintCookie:
		return fingerprint.Cookie(r)
	case FingerprintJWT:
		return fingerprint.JWT(r)
	case FingerprintHTMX:
		return fingerprint.HTMX(r)
	case FingerprintStrict:
		return fingerprint.Strict(r)
	default:
		return fingerprint.Cookie(r)
	}
}

// enforceSessionLimit deletes the oldest session if the user has reached the limit.
func (sm *sessionManager) enforceSessionLimit(ctx context.Context, userID string) error {
	if sm.config.maxSessionsPerUser == 0 {
		return nil
	}

	count, err := sm.store.CountByUserID(ctx, userID)
	if err != nil {
		return err
	}

	if count >= sm.config.maxSessionsPerUser {
		return sm.store.DeleteOldestByUserID(ctx, userID)
	}

	return nil
}
