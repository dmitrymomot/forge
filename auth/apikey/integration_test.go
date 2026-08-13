package apikey_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/apikey"
	"github.com/dmitrymomot/forge/auth/guard"
)

func TestGuardIntegration(t *testing.T) {
	t.Parallel()
	cfg := mustConfig(t, apikey.WithPrefix("sk_live"))
	mem := apikey.NewMemoryStore()
	ctx := context.Background()
	k, plaintext, err := apikey.Create(ctx, cfg,
		apikey.CreateParams{Subject: "user_42", Tenant: "org_7"}, mem.Save)
	require.NoError(t, err)

	verifier, err := apikey.NewVerifier(cfg, mem.LoadByHash, mem.Touch)
	require.NoError(t, err)

	authn := guard.New(verifier, guard.WithExtractors(guard.BearerHeader(), guard.Header("X-API-Key")))
	handler := authn(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity := guard.MustFrom(r.Context())
		w.Header().Set("X-Subject", identity.Subject)
		w.WriteHeader(http.StatusOK)
	}))

	do := func(mutate func(*http.Request)) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api", nil)
		mutate(req)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	t.Run("no credential 401", func(t *testing.T) {
		rec := do(func(*http.Request) {})
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("bearer 200", func(t *testing.T) {
		rec := do(func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+plaintext) })
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "user_42", rec.Header().Get("X-Subject"))
	})

	t.Run("X-API-Key 200", func(t *testing.T) {
		rec := do(func(r *http.Request) { r.Header.Set("X-API-Key", plaintext) })
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("garbage 401", func(t *testing.T) {
		rec := do(func(r *http.Request) { r.Header.Set("X-API-Key", "sk_live_garbage") })
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("revoked 401", func(t *testing.T) {
		require.NoError(t, apikey.Revoke(ctx, cfg, k.ID, mem.Load, mem.Revoke))
		rec := do(func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+plaintext) })
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})
}
