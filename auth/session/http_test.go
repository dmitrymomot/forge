package session_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/session"
	"github.com/dmitrymomot/forge/auth/session/transport"
)

func newWebManager(t *testing.T, opts ...session.Option) *session.Manager[data] {
	t.Helper()
	opts = append([]session.Option{session.WithTransport(transport.Cookie(transport.WithCookieName("sid")))}, opts...)
	mgr, err := session.New[data](session.NewMemoryStore(), opts...)
	require.NoError(t, err)
	return mgr
}

// cookieRequest builds a request carrying the session cookie from a prior
// response, mimicking a browser.
func cookieRequest(t *testing.T, rec *httptest.ResponseRecorder) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, ck := range rec.Result().Cookies() {
		if ck.MaxAge >= 0 && ck.Value != "" {
			r.AddCookie(&http.Cookie{Name: ck.Name, Value: ck.Value})
		}
	}
	return r
}

func TestRequestFlow_CookieBrowser(t *testing.T) {
	t.Parallel()
	mgr := newWebManager(t)

	// First visit: start + save sets the cookie.
	w1 := httptest.NewRecorder()
	r1 := httptest.NewRequest(http.MethodGet, "/", nil)
	r1.Header.Set("User-Agent", "browser-a")
	r1.RemoteAddr = "203.0.113.7:4711"
	s := mgr.Start(r1.Context())
	s.Data.Theme = "dark"
	require.NoError(t, mgr.SaveRequest(w1, r1, s))
	cks := w1.Result().Cookies()
	require.Len(t, cks, 1)
	assert.Equal(t, "sid", cks[0].Name)
	assert.Equal(t, s.Token, cks[0].Value)
	assert.True(t, cks[0].HttpOnly)
	assert.True(t, cks[0].Secure)

	// Metadata stamped for the device listing.
	assert.Equal(t, "203.0.113.7", s.IP)
	assert.Equal(t, "browser-a", s.UserAgent)
	assert.False(t, s.LastSeenAt.IsZero())

	// Next request: the browser presents the cookie.
	got, err := mgr.LoadRequest(cookieRequest(t, w1))
	require.NoError(t, err)
	assert.Equal(t, s.ID, got.ID)
	assert.Equal(t, "dark", got.Data.Theme)
	assert.Equal(t, "203.0.113.7", got.IP)

	// Login: rotation must re-set the cookie with the new token.
	w2 := httptest.NewRecorder()
	r2 := cookieRequest(t, w1)
	require.NoError(t, mgr.AuthenticateRequest(w2, r2, got, "user-1"))
	cks = w2.Result().Cookies()
	require.Len(t, cks, 1)
	assert.Equal(t, got.Token, cks[0].Value)
	assert.NotEqual(t, s.Token, cks[0].Value)

	// The old cookie is dead, the new one works.
	_, err = mgr.LoadRequest(cookieRequest(t, w1))
	assert.ErrorIs(t, err, session.ErrNotFound)
	authed, err := mgr.LoadRequest(cookieRequest(t, w2))
	require.NoError(t, err)
	assert.Equal(t, "user-1", authed.UserID)

	// Logout: destroy revokes and expires the cookie.
	w3 := httptest.NewRecorder()
	require.NoError(t, mgr.DestroyRequest(w3, cookieRequest(t, w2), authed))
	cks = w3.Result().Cookies()
	require.Len(t, cks, 1)
	assert.Negative(t, cks[0].MaxAge, "logout must expire the cookie")
	_, err = mgr.LoadRequest(cookieRequest(t, w2))
	assert.ErrorIs(t, err, session.ErrNotFound)
}

func TestRequestFlow_NoTokenIsNotFound(t *testing.T) {
	t.Parallel()
	mgr := newWebManager(t)
	_, err := mgr.LoadRequest(httptest.NewRequest(http.MethodGet, "/", nil))
	assert.ErrorIs(t, err, session.ErrNotFound)
}

func TestRequestFlow_NoTransport(t *testing.T) {
	t.Parallel()
	mgr, _, _ := newManager(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	s := mgr.Start(r.Context())

	_, err := mgr.LoadRequest(r)
	assert.ErrorIs(t, err, session.ErrNoTransport)
	assert.ErrorIs(t, mgr.SaveRequest(w, r, s), session.ErrNoTransport)
	assert.ErrorIs(t, mgr.AuthenticateRequest(w, r, s, "u"), session.ErrNoTransport)
	assert.ErrorIs(t, mgr.RotateRequest(w, r, s), session.ErrNoTransport)
	assert.ErrorIs(t, mgr.DestroyRequest(w, r, s), session.ErrNoTransport)
}

func TestRequestFlow_ClientInfoHookAndUACap(t *testing.T) {
	t.Parallel()
	custom := func(r *http.Request) (string, string) { return "10.0.0.1", r.Header.Get("X-Custom-UA") }
	mgr := newWebManager(t, session.WithClientInfo(custom))

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	longUA := make([]byte, 1000)
	for i := range longUA {
		longUA[i] = 'u'
	}
	r.Header.Set("X-Custom-UA", string(longUA))
	s := mgr.Start(r.Context())
	require.NoError(t, mgr.SaveRequest(w, r, s))
	assert.Equal(t, "10.0.0.1", s.IP)
	assert.Len(t, s.UserAgent, 256, "hostile User-Agent must be truncated")
}

func TestRequestFlow_RotateRequest(t *testing.T) {
	t.Parallel()
	mgr := newWebManager(t)
	w1 := httptest.NewRecorder()
	r1 := httptest.NewRequest(http.MethodGet, "/", nil)
	s := mgr.Start(r1.Context())
	require.NoError(t, mgr.SaveRequest(w1, r1, s))
	old := s.Token

	w2 := httptest.NewRecorder()
	require.NoError(t, mgr.RotateRequest(w2, cookieRequest(t, w1), s))
	assert.NotEqual(t, old, s.Token)
	cks := w2.Result().Cookies()
	require.Len(t, cks, 1)
	assert.Equal(t, s.Token, cks[0].Value)
}
