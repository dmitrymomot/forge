package session_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmitrymomot/forge/auth/guard"
	"github.com/dmitrymomot/forge/auth/session"
	"github.com/dmitrymomot/forge/web/middleware"
)

func TestGuardGatesOnTheAlreadyLoadedSession(t *testing.T) {
	mgr := newTestManager(t)
	authed := mgr.Start()
	if err := mgr.Authenticate(t.Context(), authed, "u1"); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	chain := middleware.Chain(
		session.Middleware(mgr, session.WithTransport(headerTransport{})),
		guard.New(session.Verifier(mgr), guard.WithExtractors(session.Extractor())),
	)

	var got guard.Identity
	h := chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = guard.MustFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Test-Token", authed.Token())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got.Subject != "u1" {
		t.Fatalf("Identity.Subject = %q, want u1", got.Subject)
	}
	if got.Method != guard.MethodSession {
		t.Fatalf("Identity.Method = %q, want %q", got.Method, guard.MethodSession)
	}
}

func TestGuard401sAnAnonymousSession(t *testing.T) {
	mgr := newTestManager(t)
	chain := middleware.Chain(
		session.Middleware(mgr, session.WithTransport(headerTransport{})),
		guard.New(session.Verifier(mgr), guard.WithExtractors(session.Extractor())),
	)

	reached := false
	h := chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	if reached {
		t.Fatal("an anonymous session must not pass the guard")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

// TestDefaultIdentityCarriesTenant pins that a scoped session's tenant reaches
// guard.Identity without WithIdentity: access.SubjectFromIdentity copies
// Identity.Tenant, so dropping it here would silently blank tenant checks in
// every downstream decider.
func TestDefaultIdentityCarriesTenant(t *testing.T) {
	mgr := newTestManager(t, session.WithScope(scopeFromCtx))
	authed := mgr.Start()
	if err := mgr.Authenticate(withTenant(t.Context(), "t1"), authed, "u1"); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	chain := middleware.Chain(
		session.Middleware(mgr, session.WithTransport(headerTransport{})),
		guard.New(session.Verifier(mgr), guard.WithExtractors(session.Extractor())),
	)

	var got guard.Identity
	h := chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = guard.MustFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest("GET", "/", nil)
	r = r.WithContext(withTenant(r.Context(), "t1"))
	r.Header.Set("X-Test-Token", authed.Token())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got.Tenant != "t1" {
		t.Fatalf("Identity.Tenant = %q, want t1", got.Tenant)
	}
}

func TestWithIdentityCarriesRoles(t *testing.T) {
	mgr := newTestManager(t)
	authed := mgr.Start()
	if err := mgr.Authenticate(t.Context(), authed, "u1"); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	nsRoles.Set(authed, rolesData{Roles: []string{"admin"}})
	if err := mgr.Save(t.Context(), authed); err != nil {
		t.Fatalf("Save: %v", err)
	}

	verifier := session.Verifier(mgr, session.WithIdentity(func(s *session.Session) guard.Identity {
		roles, _ := nsRoles.Get(s)
		return guard.Identity{Subject: s.UserID(), Roles: roles.Roles, Method: guard.MethodSession}
	}))

	chain := middleware.Chain(
		session.Middleware(mgr, session.WithTransport(headerTransport{})),
		guard.New(verifier, guard.WithExtractors(session.Extractor())),
	)

	var got guard.Identity
	h := chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = guard.MustFrom(r.Context())
	}))

	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Test-Token", authed.Token())
	h.ServeHTTP(httptest.NewRecorder(), r)

	if len(got.Roles) != 1 || got.Roles[0] != "admin" {
		t.Fatalf("Identity.Roles = %v, want [admin] — WithIdentity must reach the payload", got.Roles)
	}
}

type rolesData struct {
	Roles []string `json:"roles"`
}

var nsRoles = session.NewNamespace[rolesData]("test.roles")
