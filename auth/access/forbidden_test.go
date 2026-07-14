package access_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/access"
)

func TestWithForbiddenRendersCustomBody(t *testing.T) {
	// deny-all decider; subject supplied so the decider is reached
	deny := access.DenyAll("nope")
	sub := access.WithSubject(func(r *http.Request) (access.Subject, bool) {
		return access.Subject{ID: "u1"}, true
	})
	mw := access.RequirePermission(deny, "documents:read", sub,
		access.WithForbidden(func(w http.ResponseWriter, r *http.Request) {
			// Decision is on the context on the deny path
			dec, ok := access.DecisionFrom(r.Context())
			require.True(t, ok)
			assert.Equal(t, access.Deny, dec.Effect)
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte("<h1>Forbidden</h1>"))
		}),
	)
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler must not run on deny")
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/docs/1", nil).WithContext(context.Background())
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Equal(t, "<h1>Forbidden</h1>", rec.Body.String())
	assert.NotContains(t, rec.Header().Get("Content-Type"), "problem+json")
}
