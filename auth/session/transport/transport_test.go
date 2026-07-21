package transport_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/jwt"
	"github.com/dmitrymomot/forge/auth/session"
	"github.com/dmitrymomot/forge/auth/session/transport"
	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/crypto/keyset"
)

// Every ship transport must satisfy the seam.
var (
	_ session.Transport = (*transport.CookieTransport)(nil)
	_ session.Transport = (*transport.BearerTransport)(nil)
	_ session.Transport = (*transport.BasicTransport)(nil)
	_ session.Transport = (*transport.JWTTransport)(nil)
)

func TestCookie_RoundTrip(t *testing.T) {
	t.Parallel()
	tr := transport.Cookie()
	w := httptest.NewRecorder()
	exp := time.Now().Add(time.Hour).UTC()
	require.NoError(t, tr.Embed(w, "tok-1", exp))

	cks := w.Result().Cookies()
	require.Len(t, cks, 1)
	ck := cks[0]
	assert.Equal(t, "session", ck.Name)
	assert.Equal(t, "tok-1", ck.Value)
	assert.Equal(t, "/", ck.Path)
	assert.True(t, ck.HttpOnly)
	assert.True(t, ck.Secure)
	assert.Equal(t, http.SameSiteLaxMode, ck.SameSite)
	assert.WithinDuration(t, exp, ck.Expires, time.Second)

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(ck)
	assert.Equal(t, "tok-1", tr.Extract(r))
	assert.Empty(t, tr.Extract(httptest.NewRequest(http.MethodGet, "/", nil)))
}

func TestCookie_OptionsAndClear(t *testing.T) {
	t.Parallel()
	tr := transport.Cookie(
		transport.WithCookieName("sid"),
		transport.WithCookiePath("/app"),
		transport.WithCookieDomain("example.com"),
		transport.WithCookieSameSite(http.SameSiteStrictMode),
		transport.WithCookieSecure(false),
	)
	w := httptest.NewRecorder()
	require.NoError(t, tr.Embed(w, "tok", time.Now().Add(time.Hour)))
	ck := w.Result().Cookies()[0]
	assert.Equal(t, "sid", ck.Name)
	assert.Equal(t, "/app", ck.Path)
	assert.Equal(t, "example.com", ck.Domain)
	assert.Equal(t, http.SameSiteStrictMode, ck.SameSite)
	assert.False(t, ck.Secure)

	wc := httptest.NewRecorder()
	require.NoError(t, tr.Clear(wc))
	cleared := wc.Result().Cookies()[0]
	assert.Equal(t, "sid", cleared.Name)
	assert.Negative(t, cleared.MaxAge)
	assert.Empty(t, cleared.Value)
	assert.Equal(t, "/app", cleared.Path, "clear must match the set attributes or browsers keep the cookie")
}

func TestBearer(t *testing.T) {
	t.Parallel()
	tr := transport.Bearer()

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Bearer tok-9")
	assert.Equal(t, "tok-9", tr.Extract(r))

	lower := httptest.NewRequest(http.MethodGet, "/", nil)
	lower.Header.Set("Authorization", "bearer tok-9")
	assert.Equal(t, "tok-9", tr.Extract(lower), "scheme is case-insensitive")

	for _, h := range []string{"", "Bearer", "Bearer ", "Basic dG9rOg=="} {
		bad := httptest.NewRequest(http.MethodGet, "/", nil)
		if h != "" {
			bad.Header.Set("Authorization", h)
		}
		assert.Empty(t, tr.Extract(bad), "header %q", h)
	}

	w := httptest.NewRecorder()
	require.NoError(t, tr.Embed(w, "tok-9", time.Now()))
	assert.Equal(t, "tok-9", w.Header().Get("X-Session-Token"))
	require.NoError(t, tr.Clear(w))
	assert.Empty(t, w.Header().Get("X-Session-Token"))

	named := transport.Bearer(transport.WithBearerResponseHeader("X-Auth"))
	wn := httptest.NewRecorder()
	require.NoError(t, named.Embed(wn, "tok", time.Now()))
	assert.Equal(t, "tok", wn.Header().Get("X-Auth"))
}

func TestBasic(t *testing.T) {
	t.Parallel()
	tr := transport.Basic()

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.SetBasicAuth("anyone", "tok-3")
	assert.Equal(t, "tok-3", tr.Extract(r))
	assert.Empty(t, tr.Extract(httptest.NewRequest(http.MethodGet, "/", nil)))

	strict := transport.Basic(transport.WithBasicUsername("api"))
	assert.Empty(t, strict.Extract(r), "wrong username must extract nothing")
	ok := httptest.NewRequest(http.MethodGet, "/", nil)
	ok.SetBasicAuth("api", "tok-3")
	assert.Equal(t, "tok-3", strict.Extract(ok))

	// Client-managed credential: nothing to embed or clear.
	w := httptest.NewRecorder()
	require.NoError(t, tr.Embed(w, "tok-3", time.Now()))
	require.NoError(t, tr.Clear(w))
	assert.Empty(t, w.Header())
}

func newJWTPair(t *testing.T, vopts ...jwt.VerifierOption) (*jwt.Signer, *jwt.Verifier) {
	t.Helper()
	ks, err := keyset.New(keyset.WithPrimary(1, []byte("0123456789abcdef0123456789abcdef")))
	require.NoError(t, err)
	signer, err := jwt.NewSigner(jwt.WithHS256Keyset(ks))
	require.NoError(t, err)
	verifier, err := jwt.NewVerifier(append([]jwt.VerifierOption{jwt.WithVerifyHS256Keyset(ks)}, vopts...)...)
	require.NoError(t, err)
	return signer, verifier
}

func TestJWT_RoundTrip(t *testing.T) {
	t.Parallel()
	signer, verifier := newJWTPair(t)
	tr := transport.JWT(signer, verifier, transport.WithJWTIssuer("app"))

	w := httptest.NewRecorder()
	require.NoError(t, tr.Embed(w, "opaque-tok", time.Now().Add(time.Hour)))
	signed := w.Header().Get("X-Session-Token")
	require.NotEmpty(t, signed)
	assert.NotContains(t, signed, "opaque-tok", "the raw token must not appear in the JWT envelope verbatim")

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Bearer "+signed)
	assert.Equal(t, "opaque-tok", tr.Extract(r))
}

func TestJWT_RejectsForgedAndExpired(t *testing.T) {
	t.Parallel()
	signer, verifier := newJWTPair(t)
	tr := transport.JWT(signer, verifier)

	// Tampered signature.
	w := httptest.NewRecorder()
	require.NoError(t, tr.Embed(w, "tok", time.Now().Add(time.Hour)))
	signed := w.Header().Get("X-Session-Token")
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Bearer "+signed+"x")
	assert.Empty(t, tr.Extract(r))

	// Signed under a different key.
	otherKS, err := keyset.New(keyset.WithPrimary(1, []byte("ffffffffffffffffffffffffffffffff")))
	require.NoError(t, err)
	otherSigner, err := jwt.NewSigner(jwt.WithHS256Keyset(otherKS))
	require.NoError(t, err)
	foreign := transport.JWT(otherSigner, verifier)
	wf := httptest.NewRecorder()
	require.NoError(t, foreign.Embed(wf, "tok", time.Now().Add(time.Hour)))
	rf := httptest.NewRequest(http.MethodGet, "/", nil)
	rf.Header.Set("Authorization", "Bearer "+wf.Header().Get("X-Session-Token"))
	assert.Empty(t, tr.Extract(rf))

	// Expired envelope: verification happens against the verifier's clock.
	future := clock.NewMock(time.Now().Add(48 * time.Hour))
	_, lateVerifier := newJWTPair(t, jwt.WithClock(future))
	late := transport.JWT(signer, lateVerifier)
	rl := httptest.NewRequest(http.MethodGet, "/", nil)
	rl.Header.Set("Authorization", "Bearer "+signed)
	assert.Empty(t, late.Extract(rl), "an expired JWT envelope must extract nothing")

	// No credential at all.
	assert.Empty(t, tr.Extract(httptest.NewRequest(http.MethodGet, "/", nil)))
}
