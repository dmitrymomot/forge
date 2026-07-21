package session

import (
	"context"
	"log/slog"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/web/fingerprint"
)

// Mode selects how a fingerprint mismatch on Load is handled; see
// WithFingerprint.
type Mode int

const (
	// Warn logs the drift (session id + drifted component names) and lets the
	// session through — telemetry mode for tuning before enforcing.
	Warn Mode = iota + 1
	// Strict revokes the stored session and fails the Load with
	// ErrFingerprintMismatch. A missing current fingerprint also fails closed
	// when the session has a stored baseline.
	Strict
)

// DigestSource extracts the current request's fingerprint digest from ctx.
// ok is false when no fingerprint is available.
type DigestSource func(ctx context.Context) (fingerprint.Digest, bool)

type config struct {
	clock      clock.Clock
	scope      func(ctx context.Context) (string, error)
	logger     *slog.Logger
	digest     DigestSource
	transport  Transport
	clientInfo ClientInfo
	ttl        time.Duration
	lifetime   time.Duration
	fpMode     Mode
}

// Option configures a Manager.
type Option func(*config)

// WithTTL sets the idle timeout: each Save pushes the deadline ttl into the
// future (capped by the absolute lifetime). Default 24h.
func WithTTL(ttl time.Duration) Option { return func(c *config) { c.ttl = ttl } }

// WithLifetime sets the absolute cap counted from CreatedAt; no amount of
// activity or rotation extends a session past it. Default 30 days.
func WithLifetime(d time.Duration) Option { return func(c *config) { c.lifetime = d } }

// WithFingerprint enables hijack detection. Start (and Rotate) captures the
// current request's fingerprint digest as the session's baseline; Load
// compares the baseline against the live request and applies mode on
// mismatch. The digest comes from fingerprint.FromContext, so run
// fingerprint.Middleware ahead of session handlers (or supply
// WithDigestSource). Sessions started without an available fingerprint carry
// no baseline and are never checked.
func WithFingerprint(mode Mode) Option { return func(c *config) { c.fpMode = mode } }

// WithDigestSource overrides where WithFingerprint reads the live request's
// digest from — for consumers fingerprinting via their own middleware or
// edge/CDN headers.
func WithDigestSource(src DigestSource) Option { return func(c *config) { c.digest = src } }

// WithScope installs the tenancy hook mapping a request context to a scope
// string (e.g. the tenant id). Sessions are stamped with the scope at Save
// and invisible outside it. Resolution fails closed: a hook error or empty
// scope aborts the operation. Single-tenant apps omit this option entirely.
func WithScope(fn func(ctx context.Context) (string, error)) Option {
	return func(c *config) { c.scope = fn }
}

// WithTransport installs the client-credential transport backing the
// request-level methods (LoadRequest, SaveRequest, AuthenticateRequest,
// RotateRequest, DestroyRequest). Ship implementations live in
// session/transport (Cookie, Bearer, Basic, JWT); any Transport
// implementation plugs in. Without it the request-level methods fail with
// ErrNoTransport — the token-level API stays fully usable.
func WithTransport(t Transport) Option { return func(c *config) { c.transport = t } }

// WithClientInfo overrides how the request-level methods extract the client
// IP and User-Agent stamped onto sessions for device listings. The default
// resolves the IP via web/clientip and takes the User-Agent header verbatim
// (truncated to 256 bytes).
func WithClientInfo(fn ClientInfo) Option { return func(c *config) { c.clientInfo = fn } }

// WithLogger sets the logger for Warn-mode drift reports. Default is a no-op
// logger.
func WithLogger(l *slog.Logger) Option { return func(c *config) { c.logger = l } }

// WithClock injects a clock for tests.
func WithClock(ck clock.Clock) Option { return func(c *config) { c.clock = ck } }
