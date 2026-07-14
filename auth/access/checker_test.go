package access_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/auth/access"
)

func TestCheckerCanMatchesDecider(t *testing.T) {
	// decider allows exactly "documents:read"
	d := access.DeciderFunc(func(_ context.Context, _ access.Subject, a access.Action, _ access.Resource) (access.Decision, error) {
		if a == "documents:read" {
			return access.Allow.Because("ok"), nil
		}
		return access.Decision{Effect: access.Abstain}, nil
	})
	sub := access.WithSubject(func(r *http.Request) (access.Subject, bool) {
		return access.Subject{ID: "u1"}, true
	})

	var gotRead, gotWrite bool
	h := access.WithChecker(d, sub)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRead = access.Can(r.Context(), "documents:read")
		gotWrite = access.Can(r.Context(), "documents:write")
	}))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	assert.True(t, gotRead, "read allowed")
	assert.False(t, gotWrite, "write abstains -> not allowed")
}

func TestCanFalseWhenUnbound(t *testing.T) {
	assert.False(t, access.Can(context.Background(), "documents:read"))
}

func TestCheckerFalseWhenNoSubject(t *testing.T) {
	d := access.AllowAll()
	sub := access.WithSubject(func(r *http.Request) (access.Subject, bool) {
		return access.Subject{}, false // unauthenticated
	})
	var got bool
	h := access.WithChecker(d, sub)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = access.Can(r.Context(), "documents:read")
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	assert.False(t, got, "no subject -> not allowed even under AllowAll")
}
