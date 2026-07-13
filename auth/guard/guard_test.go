package guard_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmitrymomot/forge/auth/guard"
)

// okVerifier accepts credential "good" as user u1; everything else fails
// with errBadToken.
var errBadToken = errors.New("bad token")

func okVerifier() guard.Verifier {
	return guard.VerifierFunc(func(_ context.Context, cred string) (guard.Identity, error) {
		if cred == "good" {
			return guard.Identity{Subject: "u1", Tenant: "t1", Method: "bearer"}, nil
		}
		return guard.Identity{}, errBadToken
	})
}

// echoHandler writes the context Identity's subject, proving the request
// passed the guard and the Identity is readable.
func echoHandler(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := guard.From(r.Context())
		if !ok {
			_, _ = w.Write([]byte("anonymous"))
			return
		}
		_, _ = w.Write([]byte(id.Subject))
	})
}

func get(t *testing.T, h http.Handler, mutate func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if mutate != nil {
		mutate(r)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestNew_ValidCredential(t *testing.T) {
	t.Parallel()
	h := guard.New(okVerifier())(echoHandler(t))
	w := get(t, h, func(r *http.Request) { r.Header.Set("Authorization", "Bearer good") })
	if w.Code != http.StatusOK || w.Body.String() != "u1" {
		t.Fatalf("got %d %q, want 200 u1", w.Code, w.Body.String())
	}
}

func TestNew_MissingCredential(t *testing.T) {
	t.Parallel()
	h := guard.New(okVerifier())(echoHandler(t))
	w := get(t, h, nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
		t.Fatalf("Content-Type = %q, want application/problem+json", ct)
	}
	if w.Header().Get("WWW-Authenticate") != "" {
		t.Fatal("WWW-Authenticate set without WithChallenge")
	}
}

func TestNew_InvalidCredential(t *testing.T) {
	t.Parallel()
	h := guard.New(okVerifier())(echoHandler(t))
	w := get(t, h, func(r *http.Request) { r.Header.Set("Authorization", "Bearer evil") })
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if body := w.Body.String(); strings.Contains(body, "bad token") {
		t.Fatalf("verifier error leaked to client: %q", body)
	}
}

func TestNew_ErrorWrapping(t *testing.T) {
	t.Parallel()
	var captured error
	responder := func(w http.ResponseWriter, r *http.Request, err error) {
		captured = err
		w.WriteHeader(http.StatusUnauthorized)
	}

	h := guard.New(okVerifier(), guard.WithResponder(responder))(echoHandler(t))
	get(t, h, func(r *http.Request) { r.Header.Set("Authorization", "Bearer evil") })
	if !errors.Is(captured, guard.ErrInvalidCredential) {
		t.Fatalf("err = %v, want Is(ErrInvalidCredential)", captured)
	}
	if !errors.Is(captured, errBadToken) {
		t.Fatalf("err = %v, want Is(errBadToken) — verifier error must stay matchable", captured)
	}

	get(t, h, nil)
	if !errors.Is(captured, guard.ErrNoCredential) {
		t.Fatalf("err = %v, want Is(ErrNoCredential)", captured)
	}
}

func TestNew_EmptySubjectIsRejected(t *testing.T) {
	t.Parallel()
	var captured error
	v := guard.VerifierFunc(func(context.Context, string) (guard.Identity, error) {
		return guard.Identity{}, nil // buggy verifier: success with zero Identity
	})
	h := guard.New(v, guard.WithResponder(func(w http.ResponseWriter, r *http.Request, err error) {
		captured = err
		w.WriteHeader(http.StatusUnauthorized)
	}))(echoHandler(t))
	w := get(t, h, func(r *http.Request) { r.Header.Set("Authorization", "Bearer good") })
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if !errors.Is(captured, guard.ErrInvalidCredential) {
		t.Fatalf("err = %v, want Is(ErrInvalidCredential)", captured)
	}
}

func TestNew_Optional(t *testing.T) {
	t.Parallel()
	h := guard.New(okVerifier(), guard.WithOptional())(echoHandler(t))

	// Missing credential: anonymous pass-through.
	w := get(t, h, nil)
	if w.Code != http.StatusOK || w.Body.String() != "anonymous" {
		t.Fatalf("optional missing: got %d %q, want 200 anonymous", w.Code, w.Body.String())
	}
	// Valid credential: identity attached.
	w = get(t, h, func(r *http.Request) { r.Header.Set("Authorization", "Bearer good") })
	if w.Code != http.StatusOK || w.Body.String() != "u1" {
		t.Fatalf("optional valid: got %d %q, want 200 u1", w.Code, w.Body.String())
	}
	// Invalid credential: still 401.
	w = get(t, h, func(r *http.Request) { r.Header.Set("Authorization", "Bearer evil") })
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("optional invalid: status = %d, want 401", w.Code)
	}
}

func TestNew_Challenge(t *testing.T) {
	t.Parallel()
	h := guard.New(okVerifier(), guard.WithChallenge(`Bearer realm="api"`))(echoHandler(t))

	for name, mutate := range map[string]func(*http.Request){
		"missing": nil,
		"invalid": func(r *http.Request) { r.Header.Set("Authorization", "Bearer evil") },
	} {
		w := get(t, h, mutate)
		if got := w.Header().Get("WWW-Authenticate"); got != `Bearer realm="api"` {
			t.Fatalf("%s: WWW-Authenticate = %q, want Bearer realm=\"api\"", name, got)
		}
	}

	// Success must not carry the challenge.
	w := get(t, h, func(r *http.Request) { r.Header.Set("Authorization", "Bearer good") })
	if w.Header().Get("WWW-Authenticate") != "" {
		t.Fatal("WWW-Authenticate set on success")
	}
}

func TestNew_ExtractorChainOrder(t *testing.T) {
	t.Parallel()
	var seen []string
	v := guard.VerifierFunc(func(_ context.Context, cred string) (guard.Identity, error) {
		seen = append(seen, cred)
		return guard.Identity{Subject: cred, Method: "test"}, nil
	})
	h := guard.New(v, guard.WithExtractors(
		guard.Header("X-First"),
		guard.Header("X-Second"),
	))(echoHandler(t))

	// Both present: first extractor wins, second never consulted.
	w := get(t, h, func(r *http.Request) {
		r.Header.Set("X-First", "one")
		r.Header.Set("X-Second", "two")
	})
	if w.Body.String() != "one" {
		t.Fatalf("body = %q, want one (first extractor wins)", w.Body.String())
	}
	// First absent: chain falls through to the second.
	w = get(t, h, func(r *http.Request) { r.Header.Set("X-Second", "two") })
	if w.Body.String() != "two" {
		t.Fatalf("body = %q, want two (fallthrough)", w.Body.String())
	}
	if len(seen) != 2 {
		t.Fatalf("verifier calls = %d, want 2 (one per request)", len(seen))
	}
}

func TestNew_NoVerifyFallbackAfterExtract(t *testing.T) {
	t.Parallel()
	calls := 0
	v := guard.VerifierFunc(func(context.Context, string) (guard.Identity, error) {
		calls++
		return guard.Identity{}, errBadToken
	})
	h := guard.New(v, guard.WithExtractors(
		guard.Header("X-First"),
		guard.Header("X-Second"),
	))(echoHandler(t))

	// First extractor hits, verify fails: 401 — the second extractor must
	// NOT be offered as a fallback.
	w := get(t, h, func(r *http.Request) {
		r.Header.Set("X-First", "bad")
		r.Header.Set("X-Second", "good")
	})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if calls != 1 {
		t.Fatalf("verifier calls = %d, want 1 (no fallback after extraction)", calls)
	}
}
