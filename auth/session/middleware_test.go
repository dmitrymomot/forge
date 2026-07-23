package session_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/auth/session"
	"github.com/dmitrymomot/forge/core/clock"
)

func TestAnonymousRequestCostsNoStorage(t *testing.T) {
	store := session.NewMemoryStore()
	mgr, err := session.New(session.DefaultConfig(), session.WithStore(store))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mw := session.Middleware(mgr, session.WithTransport(headerTransport{}))

	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := mgr.For(r); !ok {
			t.Error("an anonymous visitor must still get a session object")
		}
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	if got := rec.Header().Get("X-Test-Token"); got != "" {
		t.Fatal("a request that wrote nothing must not mint a credential")
	}
}

func TestPolicyDenyIs401AndKeepsTheRecord(t *testing.T) {
	mgr := newTestManager(t)
	seed := mgr.Start()
	if err := mgr.Save(t.Context(), seed); err != nil {
		t.Fatalf("Save: %v", err)
	}

	denied := func(context.Context, *http.Request, *session.Session) error {
		return session.Deny("nope")
	}
	mw := session.Middleware(mgr,
		session.WithTransport(headerTransport{}),
		session.WithPolicy(denied),
	)

	reached := false
	h := mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))

	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Test-Token", seed.Token())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	if reached {
		t.Fatal("a denied request must not reach the handler")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if _, err := mgr.Load(t.Context(), seed.Token()); err != nil {
		t.Fatalf("Deny must leave the record intact: %v", err)
	}
}

func TestPolicyRevokeDeletesTheRecord(t *testing.T) {
	mgr := newTestManager(t)
	seed := mgr.Start()
	if err := mgr.Save(t.Context(), seed); err != nil {
		t.Fatalf("Save: %v", err)
	}

	revoked := func(context.Context, *http.Request, *session.Session) error {
		return session.Revoke("stolen")
	}
	mw := session.Middleware(mgr,
		session.WithTransport(headerTransport{}),
		session.WithPolicy(revoked),
	)
	h := mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Test-Token", seed.Token())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if _, err := mgr.Load(t.Context(), seed.Token()); !errors.Is(err, session.ErrNotFound) {
		t.Fatal("Revoke must delete the record")
	}
}

// TestPolicyReasonStaysOutOfTheResponseBody pins the sanitization: the default
// responder puts a 4xx error's text into the body, so the middleware must hand
// it ErrDenied/ErrRevoked, never the policy error whose reason explains what
// tripped detection.
func TestPolicyReasonStaysOutOfTheResponseBody(t *testing.T) {
	const secret = "fingerprint-mismatch-from-known-botnet"
	for name, policy := range map[string]session.Policy{
		"deny":   func(context.Context, *http.Request, *session.Session) error { return session.Deny(secret) },
		"revoke": func(context.Context, *http.Request, *session.Session) error { return session.Revoke(secret) },
	} {
		t.Run(name, func(t *testing.T) {
			mgr := newTestManager(t)
			seed := mgr.Start()
			if err := mgr.Save(t.Context(), seed); err != nil {
				t.Fatalf("Save: %v", err)
			}
			mw := session.Middleware(mgr,
				session.WithTransport(headerTransport{}),
				session.WithPolicy(policy),
			)
			h := mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

			r := httptest.NewRequest("GET", "/", nil)
			r.Header.Set("X-Test-Token", seed.Token())
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, r)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", rec.Code)
			}
			if strings.Contains(rec.Body.String(), secret) {
				t.Fatalf("the policy reason leaked into the response body: %s", rec.Body.String())
			}
		})
	}
}

func TestPolicyInfraErrorIs500NotAnonymous(t *testing.T) {
	mgr := newTestManager(t)
	seed := mgr.Start()
	if err := mgr.Save(t.Context(), seed); err != nil {
		t.Fatalf("Save: %v", err)
	}

	broken := func(context.Context, *http.Request, *session.Session) error {
		return errors.New("policy backend unreachable")
	}
	mw := session.Middleware(mgr,
		session.WithTransport(headerTransport{}),
		session.WithPolicy(broken),
	)

	reached := false
	h := mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))

	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Test-Token", seed.Token())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	if reached {
		t.Fatal("an infrastructure failure must never degrade to an anonymous request")
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestExpiredCredentialStartsAFreshAnonymousSession(t *testing.T) {
	mgr := newTestManager(t)
	mw := session.Middleware(mgr, session.WithTransport(headerTransport{}))

	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess := mgr.MustFor(r)
		if sess.UserID() != "" {
			t.Error("an unknown credential must not resolve to anyone")
		}
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Test-Token", "a-token-that-was-never-issued")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 — an unknown credential is anonymous, not an error", rec.Code)
	}
}

func TestNoEmbedFallsThroughToTheNextTransport(t *testing.T) {
	mgr := newTestManager(t)
	mw := session.Middleware(mgr, session.WithTransport(
		readOnlyTransport{}, // matches, cannot embed
		headerTransport{},   // embeds
	))

	seed := mgr.Start()
	if err := mgr.Save(t.Context(), seed); err != nil {
		t.Fatalf("Save: %v", err)
	}

	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess := mgr.MustFor(r)
		if err := mgr.Authenticate(r.Context(), sess, "u1"); err != nil {
			t.Errorf("Authenticate: %v", err)
		}
		http.Redirect(w, r, "/app", http.StatusSeeOther)
	}))

	r := httptest.NewRequest("GET", "/magic?t="+seed.Token(), nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	if rec.Header().Get("X-Test-Token") == "" {
		t.Fatal("a transport returning ErrNoEmbed must hand off to the first transport that can embed")
	}
}

func TestClientInfoIsPinnedAtCreationOnly(t *testing.T) {
	mgr := newTestManager(t)
	calls := 0
	mw := session.Middleware(mgr,
		session.WithTransport(headerTransport{}),
		session.WithClientInfo(func(r *http.Request) session.Bind {
			calls++
			return session.Bind{IP: r.Header.Get("X-Real-IP"), UserAgent: r.UserAgent(), Fingerprint: "fp-" + r.Header.Get("X-Real-IP")}
		}),
	)

	var token string
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess := mgr.MustFor(r)
		nsCart.Set(sess, cartData{Items: []string{"x"}})
		w.WriteHeader(http.StatusOK)
		token = sess.Token()
	}))

	first := httptest.NewRequest("GET", "/", nil)
	first.Header.Set("X-Real-IP", "203.0.113.4")
	first.Header.Set("User-Agent", "Chrome")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, first)

	saved, err := mgr.Load(t.Context(), token)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if saved.IP() != "203.0.113.4" || saved.UserAgent() != "Chrome" || saved.Fingerprint() != "fp-203.0.113.4" {
		t.Fatalf("client info not pinned: ip=%q ua=%q fp=%q", saved.IP(), saved.UserAgent(), saved.Fingerprint())
	}

	// A later request from a different address must NOT overwrite the pin,
	// or a stolen credential would rebind to the attacker on first use.
	second := httptest.NewRequest("GET", "/", nil)
	second.Header.Set("X-Test-Token", token)
	second.Header.Set("X-Real-IP", "198.51.100.9")
	second.Header.Set("User-Agent", "Firefox")
	h.ServeHTTP(httptest.NewRecorder(), second)

	after, err := mgr.Load(t.Context(), token)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if after.IP() != "203.0.113.4" || after.UserAgent() != "Chrome" {
		t.Fatalf("pinned metadata was overwritten on a later request: ip=%q ua=%q", after.IP(), after.UserAgent())
	}
}

// TestReadOnlyRequestSlidesExpiryViaToucher pins that clean traffic persists
// sliding expiry: after the touch interval, a request that dirties nothing
// still moves the stored deadline forward.
func TestReadOnlyRequestSlidesExpiryViaToucher(t *testing.T) {
	start := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	clk := clock.NewMock(start)
	store := session.NewMemoryStore()
	mgr, err := session.New(session.DefaultConfig(), session.WithStore(store),
		session.WithClock(clk), session.WithIdle(time.Hour), session.WithMaxTTL(0), session.WithTouch(5*time.Minute))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	seed := mgr.Start()
	if err := mgr.Save(t.Context(), seed); err != nil {
		t.Fatalf("Save: %v", err)
	}

	clk.Advance(10 * time.Minute)
	mw := session.Middleware(mgr, session.WithTransport(headerTransport{}))
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Test-Token", seed.Token())
	h.ServeHTTP(httptest.NewRecorder(), r)

	rec, err := store.Load(t.Context(), seed.Token())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if want := start.Add(10*time.Minute + time.Hour); !rec.ExpiresAt.Equal(want) {
		t.Fatalf("stored ExpiresAt = %v, want %v — a clean request past the touch interval must persist the slide", rec.ExpiresAt, want)
	}
}

// TestReadOnlyRequestSlidesExpiryWithoutToucher pins the fallback: with no
// Toucher capability the refresh is a full save, not a silent skip that lets
// an active session expire under read-only traffic.
func TestReadOnlyRequestSlidesExpiryWithoutToucher(t *testing.T) {
	start := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	clk := clock.NewMock(start)
	inner := session.NewMemoryStore()
	mgr, err := session.New(session.DefaultConfig(), session.WithStore(noTouchStore{inner}),
		session.WithClock(clk), session.WithIdle(time.Hour), session.WithMaxTTL(0), session.WithTouch(0))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	seed := mgr.Start()
	if err := mgr.Save(t.Context(), seed); err != nil {
		t.Fatalf("Save: %v", err)
	}

	clk.Advance(10 * time.Minute)
	mw := session.Middleware(mgr, session.WithTransport(headerTransport{}))
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Test-Token", seed.Token())
	h.ServeHTTP(httptest.NewRecorder(), r)

	rec, err := inner.Load(t.Context(), seed.Token())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if want := start.Add(10*time.Minute + time.Hour); !rec.ExpiresAt.Equal(want) {
		t.Fatalf("stored ExpiresAt = %v, want %v — WithTouch(0) and no Toucher must fall back to a full save", rec.ExpiresAt, want)
	}
}

func TestSilentHandlerCommitFailureBecomes500(t *testing.T) {
	mgr := newTestManager(t)
	mw := testMiddlewareWithCommitError(t, mgr, errors.New("store down"))

	// A handler that dirties the session but never touches the ResponseWriter:
	// no WriteHeader, Write, Flush, or Hijack. The commit runs only in the
	// post-handler fallback, and its failure must still surface as a 500 rather
	// than net/http finalizing an implicit 200.
	h := mw(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		nsCart.Set(mgr.MustFor(r), cartData{Items: []string{"x"}})
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 — a commit failure after a silent handler must not finalize as an implicit 200", rec.Code)
	}
}

// readOnlyTransport reads a query token and cannot write one back.
type readOnlyTransport struct{}

func (readOnlyTransport) Extract(r *http.Request) (string, bool) {
	tok := r.URL.Query().Get("t")
	return tok, tok != ""
}
func (readOnlyTransport) Embed(http.ResponseWriter, *http.Request, *session.Session) error {
	return session.ErrNoEmbed
}
func (readOnlyTransport) Clear(http.ResponseWriter, *http.Request) {}

// sliceKeyNoEmbedTransport is deliberately non-comparable — its slice field
// means a Go == on two Transport interface values holding this type panics
// with "comparing uncomparable type". It reads a token and cannot write one
// back, so it forces the embed fallthrough while sitting as the matched
// transport.
type sliceKeyNoEmbedTransport struct{ keys []string }

func (t sliceKeyNoEmbedTransport) Extract(r *http.Request) (string, bool) {
	tok := r.Header.Get("X-Slice-Token")
	return tok, tok != ""
}
func (sliceKeyNoEmbedTransport) Embed(http.ResponseWriter, *http.Request, *session.Session) error {
	return session.ErrNoEmbed
}
func (sliceKeyNoEmbedTransport) Clear(http.ResponseWriter, *http.Request) {}

func TestEmbedFallthroughDoesNotPanicOnANonComparableMatchedTransport(t *testing.T) {
	mgr := newTestManager(t)
	mw := session.Middleware(mgr, session.WithTransport(
		sliceKeyNoEmbedTransport{keys: []string{"a"}}, // matches, non-comparable, cannot embed
		headerTransport{}, // embeds
	))

	seed := mgr.Start()
	if err := mgr.Save(t.Context(), seed); err != nil {
		t.Fatalf("Save: %v", err)
	}

	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess := mgr.MustFor(r)
		if err := mgr.Authenticate(r.Context(), sess, "u1"); err != nil {
			t.Errorf("Authenticate: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Slice-Token", seed.Token())
	rec := httptest.NewRecorder()

	// The old `t == matched` fallthrough compared Transport interface values
	// directly; with a non-comparable dynamic type like sliceKeyNoEmbedTransport
	// that panics at runtime. A byte-index comparison never does.
	h.ServeHTTP(rec, r)

	if got := rec.Result().Header.Get("X-Test-Token"); got == "" {
		t.Fatal("a non-comparable matched transport returning ErrNoEmbed must still hand off to the next transport")
	}
}
