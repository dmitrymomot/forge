package access_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmitrymomot/forge/auth/access"
	"github.com/dmitrymomot/forge/auth/guard"
	"github.com/dmitrymomot/forge/core/ctxkey"
	"github.com/dmitrymomot/forge/web/problem"
)

// identityKey is a test-local context key (NOT guard's — ctxkey keys never
// collide across New calls). Tests seed a Subject through WithSubject +
// subjectFromContextIdentity below; the real guard.From default path is
// covered separately by TestRequirePermissionDefaultSubjectFromGuard.
var identityKey = ctxkey.New[guard.Identity]("guard")

func reqWithIdentity(id guard.Identity) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	return r.WithContext(identityKey.With(r.Context(), id))
}

func subjectFromContextIdentity(r *http.Request) (access.Subject, bool) {
	id, ok := identityKey.From(r.Context())
	if !ok {
		return access.Subject{}, false
	}
	return access.SubjectFromIdentity(id), true
}

func TestRequirePermissionAllowCallsNext(t *testing.T) {
	called := false
	var seen access.Decision
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		seen, _ = access.DecisionFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	h := access.RequirePermission(
		access.ScopeDecider(),
		"documents:read",
		access.WithSubject(subjectFromContextIdentity),
	)(next)

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, reqWithIdentity(guard.Identity{Subject: "u1", Scopes: []string{"documents:read"}}))

	if !called || rr.Code != http.StatusOK {
		t.Fatalf("want next+200, got called=%v code=%d", called, rr.Code)
	}
	if seen.Effect != access.Allow {
		t.Fatalf("decision not stashed: %+v", seen)
	}
}

func TestRequirePermissionDenyGives403(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := access.RequirePermission(
		access.ScopeDecider(),
		"documents:write",
		access.WithSubject(subjectFromContextIdentity),
	)(next)

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, reqWithIdentity(guard.Identity{Subject: "u1", Scopes: []string{"documents:read"}}))

	if rr.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d", rr.Code)
	}
}

func TestRequirePermissionMissingIdentityGives403WithoutNext(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { called = true })
	h := access.RequirePermission(access.AllowAll(), "x")(next)

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil)) // no identity

	if called || rr.Code != http.StatusForbidden {
		t.Fatalf("want 403 + no next, got called=%v code=%d", called, rr.Code)
	}
}

func TestRequirePermissionDeciderErrorGives403(t *testing.T) {
	boom := access.DeciderFunc(func(_ context.Context, _ access.Subject, _ access.Action, _ access.Resource) (access.Decision, error) {
		return access.Decision{}, errors.New("store down")
	})
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := access.RequirePermission(
		boom,
		"x",
		access.WithSubject(subjectFromContextIdentity),
	)(next)

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, reqWithIdentity(guard.Identity{Subject: "u1"}))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("want fail-closed 403, got %d", rr.Code)
	}
}

func TestWithResourceIsPassedToDecider(t *testing.T) {
	var gotRes access.Resource
	spy := access.DeciderFunc(func(_ context.Context, _ access.Subject, _ access.Action, r access.Resource) (access.Decision, error) {
		gotRes = r
		return access.Allow.Because("ok"), nil
	})
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := access.RequirePermission(spy, "documents:read",
		access.WithSubject(subjectFromContextIdentity),
		access.WithResource(func(_ *http.Request) access.Resource {
			return access.Resource{Type: "document", ID: "42"}
		}),
	)(next)

	h.ServeHTTP(httptest.NewRecorder(), reqWithIdentity(guard.Identity{Subject: "u1"}))
	if gotRes.Type != "document" || gotRes.ID != "42" {
		t.Fatalf("resource not resolved: %+v", gotRes)
	}
}

func TestRequireDynamicAction(t *testing.T) {
	var gotAction access.Action
	spy := access.DeciderFunc(func(_ context.Context, _ access.Subject, a access.Action, _ access.Resource) (access.Decision, error) {
		gotAction = a
		return access.Allow.Because("ok"), nil
	})
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := access.Require(
		spy,
		func(_ *http.Request) (access.Action, access.Resource) {
			return "custom:action", access.Resource{Type: "doc"}
		},
		access.WithSubject(subjectFromContextIdentity),
	)(next)

	h.ServeHTTP(httptest.NewRecorder(), reqWithIdentity(guard.Identity{Subject: "u1"}))
	if gotAction != "custom:action" {
		t.Fatalf("dynamic action not used: %q", gotAction)
	}
}

func TestWithExplainStashesTrace(t *testing.T) {
	d := access.FirstDecisive(access.TenantMatch(), access.ScopeDecider())
	var seen access.Decision
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen, _ = access.DecisionFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	h := access.RequirePermission(
		d,
		"documents:read",
		access.WithSubject(subjectFromContextIdentity),
		access.WithExplain(),
	)(next)
	h.ServeHTTP(httptest.NewRecorder(), reqWithIdentity(guard.Identity{Subject: "u1", Scopes: []string{"documents:read"}}))
	if len(seen.Trace) == 0 {
		t.Fatalf("want trace stashed under WithExplain, got %+v", seen)
	}
}

func TestRequirePermissionDefaultSubjectFromGuard(t *testing.T) {
	verifier := guard.VerifierFunc(func(_ context.Context, cred string) (guard.Identity, error) {
		if cred == "good" {
			return guard.Identity{Subject: "u1", Scopes: []string{"documents:read"}}, nil
		}
		return guard.Identity{}, errors.New("bad token")
	})
	called := false
	var seen access.Decision
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		seen, _ = access.DecisionFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	h := guard.New(verifier)(
		access.RequirePermission(access.ScopeDecider(), "documents:read")(next),
	)
	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Bearer good")
	h.ServeHTTP(rr, r)
	if !called || rr.Code != http.StatusOK {
		t.Fatalf("want next+200, got called=%v code=%d", called, rr.Code)
	}
	if seen.Effect != access.Allow {
		t.Fatalf("want Allow decision, got %+v", seen)
	}
}

func TestWithResponderIsUsed(t *testing.T) {
	custom := problem.Responder(func(w http.ResponseWriter, _ *http.Request, _ error) {
		w.WriteHeader(http.StatusTeapot)
	})
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := access.RequirePermission(
		access.DenyAll("no"),
		"documents:read",
		access.WithSubject(subjectFromContextIdentity),
		access.WithResponder(custom),
	)(next)

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, reqWithIdentity(guard.Identity{Subject: "u1"}))
	if rr.Code != http.StatusTeapot {
		t.Fatalf("want custom responder's 418, got %d", rr.Code)
	}
}

func TestWithLoggerLogsDeciderError(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	boom := access.DeciderFunc(func(_ context.Context, _ access.Subject, _ access.Action, _ access.Resource) (access.Decision, error) {
		return access.Decision{}, errors.New("store down")
	})
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := access.RequirePermission(
		boom,
		"documents:read",
		access.WithSubject(subjectFromContextIdentity),
		access.WithLogger(logger),
	)(next)

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, reqWithIdentity(guard.Identity{Subject: "u1"}))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d", rr.Code)
	}
	if !strings.Contains(buf.String(), "access decider error") {
		t.Fatalf("want decider error logged, got: %s", buf.String())
	}
}
