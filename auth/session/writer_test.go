package session_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmitrymomot/forge/auth/session"
	"github.com/dmitrymomot/forge/web/middleware"
)

func TestCommitRunsBeforeRedirectHeadersGoOut(t *testing.T) {
	mgr := newTestManager(t)
	mw := testMiddleware(t, mgr)

	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess := mgr.MustFor(r)
		nsCart.Set(sess, cartData{Items: []string{"x"}})
		http.Redirect(w, r, "/app", http.StatusSeeOther)
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/login", nil))

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	if rec.Header().Get("Location") != "/app" {
		t.Fatalf("Location = %q, want /app", rec.Header().Get("Location"))
	}
	if got := rec.Header().Get("X-Test-Token"); got == "" {
		t.Fatal("the credential must be embedded on a 303 — a defer-based commit would silently lose it")
	}
}

func TestCommitFailureBecomes500AndSuppressesTheBody(t *testing.T) {
	mgr := newTestManager(t)
	mw := testMiddlewareWithCommitError(t, mgr, errors.New("store down"))

	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess := mgr.MustFor(r)
		nsCart.Set(sess, cartData{Items: []string{"x"}})
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("sensitive body"))
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 — a failed commit must never look like success", rec.Code)
	}
	if rec.Body.String() == "sensitive body" {
		t.Fatal("the handler's body must not be written after the commit failed")
	}
}

func TestFlushCommitsFirst(t *testing.T) {
	mgr := newTestManager(t)
	mw := testMiddleware(t, mgr)

	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess := mgr.MustFor(r)
		nsCart.Set(sess, cartData{Items: []string{"x"}})
		// An SSE handler flushes before writing anything else.
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Errorf("Flush: %v", err)
		}
		_, _ = w.Write([]byte("data: hello\n\n"))
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/events", nil))

	if rec.Result().Header.Get("X-Test-Token") == "" {
		t.Fatal("a Flush before the first Write must still commit — ResponseController bypasses WriteHeader")
	}
}

type headerTransport struct{ embedErr error }

func (h headerTransport) Extract(r *http.Request) (string, bool) {
	tok := r.Header.Get("X-Test-Token")
	return tok, tok != ""
}

func (h headerTransport) Embed(w http.ResponseWriter, _ *http.Request, s *session.Session) error {
	if h.embedErr != nil {
		return h.embedErr
	}
	w.Header().Set("X-Test-Token", s.Token())
	return nil
}

func (h headerTransport) Clear(w http.ResponseWriter, _ *http.Request) {
	w.Header().Del("X-Test-Token")
}

func testMiddleware(t *testing.T, mgr *session.Manager) middleware.Middleware {
	t.Helper()
	return session.Middleware(mgr, session.WithTransport(headerTransport{}))
}

func testMiddlewareWithCommitError(t *testing.T, mgr *session.Manager, err error) middleware.Middleware {
	t.Helper()
	return session.Middleware(mgr, session.WithTransport(headerTransport{embedErr: err}))
}
