package access_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmitrymomot/forge/auth/access"
	"github.com/dmitrymomot/forge/auth/guard"
)

type doc struct {
	ID      string
	Tenant  string
	OwnerID string
}

func docModel(load func(*http.Request) (doc, error)) access.Model[doc] {
	return access.NewModel(load, func(d doc) access.Resource {
		return access.Resource{Type: "document", ID: d.ID, Tenant: d.Tenant, Attrs: map[string]any{"owner_id": d.OwnerID}}
	})
}

func TestModelHandleAllowInjectsObject(t *testing.T) {
	m := docModel(func(_ *http.Request) (doc, error) { return doc{ID: "1", OwnerID: "u1"}, nil })
	var got doc
	h := m.Handle(access.AllowAll(), "documents:read", func(w http.ResponseWriter, r *http.Request, d doc) {
		if dec, ok := access.DecisionFrom(r.Context()); !ok || dec.Effect != access.Allow {
			t.Errorf("decision not stashed for Model.Handle: ok=%v dec=%+v", ok, dec)
		}
		got = d
		w.WriteHeader(http.StatusOK)
	}, access.WithSubject(subjectFromContextIdentity))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, reqWithIdentity(guard.Identity{Subject: "u1"}))
	if rr.Code != http.StatusOK || got.ID != "1" {
		t.Fatalf("want injected doc + 200, got code=%d doc=%+v", rr.Code, got)
	}
}

func TestModelHandleDenyGives403(t *testing.T) {
	m := docModel(func(_ *http.Request) (doc, error) { return doc{ID: "1"}, nil })
	called := false
	h := m.Handle(access.DenyAll("no"), "documents:write", func(w http.ResponseWriter, _ *http.Request, _ doc) { called = true }, access.WithSubject(subjectFromContextIdentity))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, reqWithIdentity(guard.Identity{Subject: "u1"}))
	if called || rr.Code != http.StatusForbidden {
		t.Fatalf("want 403 + no fn, got called=%v code=%d", called, rr.Code)
	}
}

func TestModelHandleLoadErrorGives404(t *testing.T) {
	loaded := false
	secret := errors.New("sql: dial tcp 10.0.0.1 refused SEKRET")
	m := docModel(func(_ *http.Request) (doc, error) { loaded = true; return doc{}, secret })
	h := m.Handle(access.AllowAll(), "documents:read", func(w http.ResponseWriter, _ *http.Request, _ doc) {}, access.WithSubject(subjectFromContextIdentity))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, reqWithIdentity(guard.Identity{Subject: "u1"}))
	if !loaded || rr.Code != http.StatusNotFound {
		t.Fatalf("want 404, got loaded=%v code=%d", loaded, rr.Code)
	}
	if body := rr.Body.String(); strings.Contains(body, "SEKRET") {
		t.Fatalf("default load-error response leaked raw error text: %s", body)
	}
}

func TestModelHandleMissingSubjectSkipsLoad(t *testing.T) {
	loaded := false
	m := docModel(func(_ *http.Request) (doc, error) { loaded = true; return doc{}, nil })
	h := m.Handle(access.AllowAll(), "documents:read", func(w http.ResponseWriter, _ *http.Request, _ doc) {})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil)) // no identity
	if loaded || rr.Code != http.StatusForbidden {
		t.Fatalf("want 403 without loading, got loaded=%v code=%d", loaded, rr.Code)
	}
}

func TestModelHandleDeciderErrorGives403(t *testing.T) {
	m := docModel(func(_ *http.Request) (doc, error) { return doc{ID: "1"}, nil })
	called := false
	boom := access.DeciderFunc(func(_ context.Context, _ access.Subject, _ access.Action, _ access.Resource) (access.Decision, error) {
		return access.Decision{}, errors.New("store down")
	})
	h := m.Handle(boom, "documents:read", func(_ http.ResponseWriter, _ *http.Request, _ doc) { called = true },
		access.WithSubject(subjectFromContextIdentity))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, reqWithIdentity(guard.Identity{Subject: "u1"}))
	if called || rr.Code != http.StatusForbidden {
		t.Fatalf("want fail-closed 403 + no fn, got called=%v code=%d", called, rr.Code)
	}
}

func TestNewModelPanicsOnNilFuncs(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("want panic on nil load")
		}
	}()
	access.NewModel[doc](nil, func(d doc) access.Resource { return access.Resource{} })
}

func TestNewModelPanicsOnNilDescribe(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("want panic on nil describe")
		}
	}()
	access.NewModel(func(_ *http.Request) (doc, error) { return doc{}, nil }, nil)
}

func TestModelHandlePanicsOnWithResource(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("want panic when WithResource is passed to Model.Handle")
		}
	}()
	m := docModel(func(_ *http.Request) (doc, error) { return doc{ID: "1"}, nil })
	m.Handle(access.AllowAll(), "documents:read",
		func(http.ResponseWriter, *http.Request, doc) {},
		access.WithResource(func(*http.Request) access.Resource { return access.Resource{} }),
	)
}
