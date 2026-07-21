package session_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/session"
	"github.com/dmitrymomot/forge/core/ctxkey"
	"github.com/dmitrymomot/forge/web/fingerprint"
)

var digestKey = ctxkey.New[fingerprint.Digest]("digest")

// ctxDigest reads the test's digest straight from context, standing in for
// fingerprint.Middleware.
func ctxDigest(ctx context.Context) (fingerprint.Digest, bool) { return digestKey.From(ctx) }

func digestCtx(t *testing.T, hash string, parts map[string]string) context.Context {
	t.Helper()
	return digestKey.With(t.Context(), fingerprint.Digest{Hash: hash, Parts: parts, Version: 1})
}

func newFPManager(t *testing.T, mode session.Mode, opts ...session.Option) (*session.Manager[data], *session.MemoryStore) {
	t.Helper()
	store := session.NewMemoryStore()
	opts = append([]session.Option{session.WithFingerprint(mode), session.WithDigestSource(ctxDigest)}, opts...)
	mgr, err := session.New[data](store, opts...)
	require.NoError(t, err)
	return mgr, store
}

func TestFingerprint_MatchPasses(t *testing.T) {
	t.Parallel()
	for _, mode := range []session.Mode{session.Warn, session.Strict} {
		mgr, _ := newFPManager(t, mode)
		ctx := digestCtx(t, "h1", map[string]string{"ua": "a"})
		s := mgr.Start(ctx)
		require.NoError(t, mgr.Save(ctx, s))
		_, err := mgr.Load(ctx, s.Token)
		require.NoError(t, err)
	}
}

func TestFingerprint_StrictMismatchRevokes(t *testing.T) {
	t.Parallel()
	mgr, _ := newFPManager(t, session.Strict)
	ctx := digestCtx(t, "h1", map[string]string{"ua": "a"})
	s := mgr.Start(ctx)
	require.NoError(t, mgr.Save(ctx, s))

	hijacked := digestCtx(t, "h2", map[string]string{"ua": "b"})
	_, err := mgr.Load(hijacked, s.Token)
	assert.ErrorIs(t, err, session.ErrFingerprintMismatch)

	// The session must be revoked, not merely refused: even the original
	// client is out.
	_, err = mgr.Load(ctx, s.Token)
	assert.ErrorIs(t, err, session.ErrNotFound)
}

func TestFingerprint_StrictMissingDigestFailsClosed(t *testing.T) {
	t.Parallel()
	mgr, _ := newFPManager(t, session.Strict)
	ctx := digestCtx(t, "h1", nil)
	s := mgr.Start(ctx)
	require.NoError(t, mgr.Save(ctx, s))

	// A baselined session presented without any live fingerprint is treated
	// as hijacked, not waved through.
	_, err := mgr.Load(t.Context(), s.Token)
	assert.ErrorIs(t, err, session.ErrFingerprintMismatch)
}

func TestFingerprint_WarnLogsAndPasses(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	mgr, _ := newFPManager(t, session.Warn, session.WithLogger(log))

	ctx := digestCtx(t, "h1", map[string]string{"ua": "a", "ip": "x"})
	s := mgr.Start(ctx)
	require.NoError(t, mgr.Save(ctx, s))

	drifted := digestCtx(t, "h2", map[string]string{"ua": "b", "ip": "x"})
	got, err := mgr.Load(drifted, s.Token)
	require.NoError(t, err, "warn mode must let the session through")
	assert.Equal(t, s.ID, got.ID)
	assert.Contains(t, buf.String(), "fingerprint drift")
	assert.Contains(t, buf.String(), "ua", "drifted component names belong in the log")
}

func TestFingerprint_NoBaselineSkipsCheck(t *testing.T) {
	t.Parallel()
	mgr, _ := newFPManager(t, session.Strict)

	// Started without any available fingerprint: no baseline, never checked.
	s := mgr.Start(t.Context())
	require.NoError(t, mgr.Save(t.Context(), s))
	_, err := mgr.Load(digestCtx(t, "h1", nil), s.Token)
	require.NoError(t, err)
}

func TestFingerprint_RotateRecapturesBaseline(t *testing.T) {
	t.Parallel()
	mgr, _ := newFPManager(t, session.Strict)
	oldCtx := digestCtx(t, "h1", nil)
	s := mgr.Start(oldCtx)
	require.NoError(t, mgr.Save(oldCtx, s))

	// Login from a new device: Authenticate rotates and adopts its digest.
	newCtx := digestCtx(t, "h2", nil)
	require.NoError(t, mgr.Authenticate(newCtx, s, "user-1"))
	_, err := mgr.Load(newCtx, s.Token)
	require.NoError(t, err)
	_, err = mgr.Load(oldCtx, s.Token)
	assert.ErrorIs(t, err, session.ErrFingerprintMismatch)
}

// failDeleteStore fails Delete while delegating everything else.
type failDeleteStore struct {
	session.Store
	err error
}

func (f *failDeleteStore) Delete(ctx context.Context, token string) error {
	if f.err != nil {
		return f.err
	}
	return f.Store.Delete(ctx, token)
}

func TestFingerprint_RotateSaveFailureRestoresBaseline(t *testing.T) {
	t.Parallel()
	store := session.NewMemoryStore()
	fs := &failSaveStore{Store: store}
	mgr, err := session.New[data](fs,
		session.WithFingerprint(session.Strict), session.WithDigestSource(ctxDigest))
	require.NoError(t, err)

	ctxA := digestCtx(t, "h1", nil)
	s := mgr.Start(ctxA)
	require.NoError(t, mgr.Save(ctxA, s))

	// A rotation from a new device fails at the store; the in-memory session
	// must roll back to the baseline the store still holds.
	fs.err = errors.New("boom")
	require.Error(t, mgr.Rotate(digestCtx(t, "h2", nil), s))
	fs.err = nil

	require.NoError(t, mgr.Save(ctxA, s))
	_, err = mgr.Load(ctxA, s.Token)
	require.NoError(t, err, "the original device must still match after a failed rotation")
}

// failSaveStore fails Save while delegating everything else.
type failSaveStore struct {
	session.Store
	err error
}

func (f *failSaveStore) Save(ctx context.Context, token string, rec session.Record) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.Store.Save(ctx, token, rec)
}

func TestFingerprint_StrictRevokeFailureKeepsMismatchSignal(t *testing.T) {
	t.Parallel()
	fs := &failDeleteStore{Store: session.NewMemoryStore()}
	mgr, err := session.New[data](fs,
		session.WithFingerprint(session.Strict), session.WithDigestSource(ctxDigest))
	require.NoError(t, err)

	ctx := digestCtx(t, "h1", nil)
	s := mgr.Start(ctx)
	require.NoError(t, mgr.Save(ctx, s))

	fs.err = errors.New("boom")
	_, err = mgr.Load(digestCtx(t, "h2", nil), s.Token)
	assert.ErrorIs(t, err, session.ErrFingerprintMismatch, "a failed revoke must not swallow the hijack signal")
	assert.ErrorIs(t, err, session.ErrStore, "and the revoke failure must surface too")
}

// TestFingerprint_DefaultSourceRidesMiddleware proves the default digest
// source picks up web/fingerprint.Middleware without any wiring.
func TestFingerprint_DefaultSourceRidesMiddleware(t *testing.T) {
	t.Parallel()
	fp, err := fingerprint.Session(fingerprint.Config{Secret: "0123456789abcdef0123456789abcdef", Version: 1, TokenTTL: time.Minute})
	require.NoError(t, err)
	mgr, err := session.New[data](session.NewMemoryStore(), session.WithFingerprint(session.Strict))
	require.NoError(t, err)

	var token string
	var loadErr error
	mux := http.NewServeMux()
	mux.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) {
		s := mgr.Start(r.Context())
		require.NoError(t, mgr.Save(r.Context(), s))
		token = s.Token
	})
	mux.HandleFunc("/load", func(w http.ResponseWriter, r *http.Request) {
		_, loadErr = mgr.Load(r.Context(), token)
	})
	handler := fp.Middleware()(mux)

	do := func(path, ua string) {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("User-Agent", ua)
		handler.ServeHTTP(httptest.NewRecorder(), req)
	}

	do("/start", "browser-a")
	require.NotEmpty(t, token)
	do("/load", "browser-a")
	require.NoError(t, loadErr)
	do("/load", "browser-b")
	assert.ErrorIs(t, loadErr, session.ErrFingerprintMismatch)
}
