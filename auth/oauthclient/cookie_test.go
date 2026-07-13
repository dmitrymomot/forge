package oauthclient_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/oauthclient"
)

func TestBeginSetsCookieAndRedirects(t *testing.T) {
	f := newFakeOIDC(t)
	c := f.newClient(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "https://app.example.com/auth/idp", nil)
	require.NoError(t, c.Begin(rec, req, "idp", oauthclient.WithReturnTo("/dash")))

	assert.Equal(t, http.StatusSeeOther, rec.Code)
	loc, err := url.Parse(rec.Header().Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "/authorize", loc.Path)

	cookies := rec.Result().Cookies()
	require.Len(t, cookies, 1)
	ck := cookies[0]
	assert.Equal(t, "oauth_flow", ck.Name)
	assert.NotEmpty(t, ck.Value)
	assert.True(t, ck.HttpOnly)
	assert.True(t, ck.Secure)
	assert.Equal(t, http.SameSiteLaxMode, ck.SameSite)
	assert.Equal(t, "/", ck.Path)
	assert.Equal(t, 600, ck.MaxAge)
}

func TestCompleteHappyPath(t *testing.T) {
	f := newFakeOIDC(t)
	c := f.newClient(t)

	// Begin
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "https://app.example.com/auth/idp", nil)
	require.NoError(t, c.Begin(rec, req, "idp"))
	flowCookie := rec.Result().Cookies()[0]
	loc, _ := url.Parse(rec.Header().Get("Location"))
	withNonce(f, loc.Query())

	// Callback
	cbURL := "https://app.example.com/cb?code=code-1&state=" + url.QueryEscape(loc.Query().Get("state"))
	cbReq := httptest.NewRequest(http.MethodGet, cbURL, nil)
	cbReq.AddCookie(flowCookie)
	cbRec := httptest.NewRecorder()

	res, err := c.Complete(cbRec, cbReq)
	require.NoError(t, err)
	assert.Equal(t, "user-1", res.Identity.Subject)

	// flow cookie is cleared
	cleared := cbRec.Result().Cookies()
	require.Len(t, cleared, 1)
	assert.Equal(t, "oauth_flow", cleared[0].Name)
	assert.Equal(t, -1, cleared[0].MaxAge)
}

func TestCompleteWithoutCookie(t *testing.T) {
	f := newFakeOIDC(t)
	c := f.newClient(t)
	req := httptest.NewRequest(http.MethodGet, "https://app.example.com/cb?code=x&state=y", nil)
	_, err := c.Complete(httptest.NewRecorder(), req)
	require.ErrorIs(t, err, oauthclient.ErrFlowExpired)
}

func TestBeginCustomCookieName(t *testing.T) {
	f := newFakeOIDC(t)
	c := f.newClient(t, oauthclient.WithCookieName("my_flow"))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "https://app.example.com/auth/idp", nil)
	require.NoError(t, c.Begin(rec, req, "idp"))
	assert.Equal(t, "my_flow", rec.Result().Cookies()[0].Name)
}
