package apikey_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/apikey"
	"github.com/dmitrymomot/forge/auth/guard"
)

// guardedHandler wires NewVerifier into guard exactly as an application
// does, and echoes the resolved subject.
func guardedHandler(t *testing.T, cfg apikey.Config, load apikey.LoadByHashFunc) http.Handler {
	t.Helper()
	verifier, err := apikey.NewVerifier(cfg, load, nil)
	require.NoError(t, err)

	authn := guard.New(verifier, guard.WithExtractors(guard.BearerHeader(), guard.Header("X-API-Key")))
	return authn(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Subject", guard.MustFrom(r.Context()).Subject)
		w.WriteHeader(http.StatusOK)
	}))
}

func serve(h http.Handler, mutate func(*http.Request)) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api", nil)
	mutate(req)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestGuard_RejectsRequestWithoutCredential(t *testing.T) {
	t.Parallel()
	cfg, k, _ := verifiable(t)
	h := guardedHandler(t, cfg, loadsKeyByHash(k))

	rec := serve(h, func(*http.Request) {})
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestGuard_AcceptsBearerCredential(t *testing.T) {
	t.Parallel()
	cfg, k, plaintext := verifiable(t)
	h := guardedHandler(t, cfg, loadsKeyByHash(k))

	rec := serve(h, func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+plaintext) })
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "user_42", rec.Header().Get("X-Subject"))
}

func TestGuard_AcceptsAPIKeyHeader(t *testing.T) {
	t.Parallel()
	cfg, k, plaintext := verifiable(t)
	h := guardedHandler(t, cfg, loadsKeyByHash(k))

	rec := serve(h, func(r *http.Request) { r.Header.Set("X-API-Key", plaintext) })
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestGuard_RejectsGarbageCredential(t *testing.T) {
	t.Parallel()
	cfg, k, _ := verifiable(t)
	h := guardedHandler(t, cfg, loadsKeyByHash(k))

	rec := serve(h, func(r *http.Request) { r.Header.Set("X-API-Key", "sk_live_garbage") })
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestGuard_RejectsRevokedCredential(t *testing.T) {
	t.Parallel()
	cfg, k, plaintext := verifiable(t)
	k.RevokedAt = time.Now().UTC()
	h := guardedHandler(t, cfg, loadsKeyByHash(k))

	rec := serve(h, func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+plaintext) })
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}
