package oauthserver_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/oauthserver"
)

func BenchmarkTokenClientCredentials(b *testing.B) {
	srv, _ := newServer(b)
	creds, err := srv.CreateClient(context.Background(), oauthserver.CreateClientInput{
		Name: "p", Grants: []string{oauthserver.GrantClientCredentials}, Scopes: []string{"read"},
	})
	require.NoError(b, err)
	h := srv.TokenHandler()
	form := url.Values{"grant_type": {"client_credentials"}}.Encode()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		req := httptest.NewRequest(http.MethodPost, "/token", strings.NewReader(form))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.SetBasicAuth(creds.ClientID, creds.ClientSecret)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			b.Fatalf("status %d", rec.Code)
		}
	}
}

func BenchmarkTokenBadSecret(b *testing.B) {
	// The rejection path must stay cheap: it is the brute-force surface.
	srv, _ := newServer(b)
	creds, err := srv.CreateClient(context.Background(), oauthserver.CreateClientInput{
		Name: "p", Grants: []string{oauthserver.GrantClientCredentials},
	})
	require.NoError(b, err)
	h := srv.TokenHandler()
	form := url.Values{"grant_type": {"client_credentials"}}.Encode()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		req := httptest.NewRequest(http.MethodPost, "/token", strings.NewReader(form))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.SetBasicAuth(creds.ClientID, "osk_wrong")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			b.Fatalf("status %d", rec.Code)
		}
	}
}
