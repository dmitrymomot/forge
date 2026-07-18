package smartlink_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/web/smartlink"
)

// newMuxHandler mounts h on a pattern with a {code} wildcard, so
// r.PathValue("code") is populated the way a real router would.
func newMuxHandler(h http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/r/{code}", h)
	return mux
}

// TestHandlerTargetRedirect asserts a Target-backed Link redirects with the
// configured status, the resolved Location, and a no-store cache header.
func TestHandlerTargetRedirect(t *testing.T) {
	t.Parallel()
	m := newTestManager(t)
	l, err := m.Create(context.Background(), smartlink.CreateParams{Target: "https://dest.example.com/"})
	if err != nil {
		t.Fatalf("Create() error = %v, want nil", err)
	}
	h := newMuxHandler(m.Handler())

	req := httptest.NewRequest(http.MethodGet, "/r/"+l.Code, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusFound)
	}
	if got, want := rec.Header().Get("Location"), "https://dest.example.com/"; got != want {
		t.Fatalf("Location = %q, want %q", got, want)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want %q", got, "no-store")
	}
}

// TestHandlerParamForwarding asserts an incoming query param forwards under
// the default ParamsFill policy, but the target's own param wins on collision.
func TestHandlerParamForwarding(t *testing.T) {
	t.Parallel()
	m := newTestManager(t)
	l, err := m.Create(context.Background(), smartlink.CreateParams{Target: "https://dest.example.com/lp?click_id=own"})
	if err != nil {
		t.Fatalf("Create() error = %v, want nil", err)
	}
	h := newMuxHandler(m.Handler())

	req := httptest.NewRequest(http.MethodGet, "/r/"+l.Code+"?click_id=incoming&extra=1", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	loc := rec.Header().Get("Location")
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("Location %q unparseable: %v", loc, err)
	}
	q := u.Query()
	if got, want := q.Get("click_id"), "own"; got != want {
		t.Fatalf("click_id = %q, want %q (target's own param must win on collision)", got, want)
	}
	if got, want := q.Get("extra"), "1"; got != want {
		t.Fatalf("extra = %q, want %q (incoming param must forward)", got, want)
	}
}

// TestHandlerMetadataWins asserts the link's Metadata overrides a same-key
// query param when both reach the {param.NAME} macro.
func TestHandlerMetadataWins(t *testing.T) {
	t.Parallel()
	m := newTestManager(t)
	l, err := m.Create(context.Background(), smartlink.CreateParams{
		Target:   "https://dest.example.com/lp?aff={param.affiliate_id}",
		Metadata: map[string]string{"affiliate_id": "meta-value"},
	})
	if err != nil {
		t.Fatalf("Create() error = %v, want nil", err)
	}
	h := newMuxHandler(m.Handler())

	req := httptest.NewRequest(http.MethodGet, "/r/"+l.Code+"?affiliate_id=query-value", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "aff=meta-value") {
		t.Fatalf("Location = %q, want metadata value (meta-value) to win over the query param", loc)
	}
	if strings.Contains(loc, "query-value") {
		t.Fatalf("Location = %q, must not carry the shadowed query value", loc)
	}
}

// geoResolver returns a Resolver whose Spec redirects DE visitors to a
// distinct target, falling back to a default target otherwise.
func geoResolver() smartlink.Resolver {
	return func(context.Context, smartlink.Link) (smartlink.Decider, error) {
		return smartlink.Compile(smartlink.Spec{
			Rules: []smartlink.Rule{{
				Name:    "de",
				When:    []smartlink.Matcher{smartlink.Geo{Countries: []string{"DE"}}},
				Targets: []smartlink.Target{{URL: "https://de.example.com/"}},
			}},
			Default: []smartlink.Target{{URL: "https://default.example.com/"}},
		})
	}
}

// TestHandlerVisitFuncEnriches asserts a geo rule matches only when a
// configured VisitFunc sets Visit.Country from the request; without one, the
// same request falls through to the default target.
func TestHandlerVisitFuncEnriches(t *testing.T) {
	t.Parallel()

	t.Run("with VisitFunc", func(t *testing.T) {
		t.Parallel()
		visitFunc := func(r *http.Request, v smartlink.Visit) smartlink.Visit {
			v.Country = r.Header.Get("X-Country")
			return v
		}
		m := newTestManager(t, smartlink.WithResolver(geoResolver()), smartlink.WithVisitFunc(visitFunc))
		l, err := m.Create(context.Background(), smartlink.CreateParams{Ref: "geo-1"})
		if err != nil {
			t.Fatalf("Create() error = %v, want nil", err)
		}
		h := newMuxHandler(m.Handler())

		req := httptest.NewRequest(http.MethodGet, "/r/"+l.Code, nil)
		req.Header.Set("X-Country", "DE")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if got, want := rec.Header().Get("Location"), "https://de.example.com/"; got != want {
			t.Fatalf("Location = %q, want %q (geo rule target)", got, want)
		}
	})

	t.Run("without VisitFunc falls to default", func(t *testing.T) {
		t.Parallel()
		m := newTestManager(t, smartlink.WithResolver(geoResolver()))
		l, err := m.Create(context.Background(), smartlink.CreateParams{Ref: "geo-1"})
		if err != nil {
			t.Fatalf("Create() error = %v, want nil", err)
		}
		h := newMuxHandler(m.Handler())

		req := httptest.NewRequest(http.MethodGet, "/r/"+l.Code, nil)
		req.Header.Set("X-Country", "DE") // ignored: no VisitFunc reads it
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if got, want := rec.Header().Get("Location"), "https://default.example.com/"; got != want {
			t.Fatalf("Location = %q, want %q (default target)", got, want)
		}
	})
}

// TestHandlerRefResolves is an end-to-end test of a Ref-backed Link through
// a real Cache-backed Resolver (Task 4's Cache.Resolver).
func TestHandlerRefResolves(t *testing.T) {
	t.Parallel()
	c := mustNewCache(t, func(_ context.Context, ref string) (smartlink.Spec, error) {
		return smartlink.Spec{Default: []smartlink.Target{{URL: "https://offer.example.com/" + ref}}}, nil
	})
	m := newTestManager(t, smartlink.WithResolver(c.Resolver()))
	l, err := m.Create(context.Background(), smartlink.CreateParams{Ref: "offer-42"})
	if err != nil {
		t.Fatalf("Create() error = %v, want nil", err)
	}
	h := newMuxHandler(m.Handler())

	req := httptest.NewRequest(http.MethodGet, "/r/"+l.Code, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusFound)
	}
	if got, want := rec.Header().Get("Location"), "https://offer.example.com/offer-42"; got != want {
		t.Fatalf("Location = %q, want %q", got, want)
	}
}

// TestHandlerRefNoTarget asserts a Resolver error wrapping ErrNoTarget is
// dead-link handling: fallback redirect when configured, else 404.
func TestHandlerRefNoTarget(t *testing.T) {
	t.Parallel()
	resolver := func(context.Context, smartlink.Link) (smartlink.Decider, error) {
		return nil, fmt.Errorf("offer paused: %w", smartlink.ErrNoTarget)
	}

	t.Run("with fallback", func(t *testing.T) {
		t.Parallel()
		m := newTestManager(t, smartlink.WithResolver(resolver), smartlink.WithFallbackURL("https://fallback.example.com/"))
		l, err := m.Create(context.Background(), smartlink.CreateParams{Ref: "paused-1", SkipRefCheck: true})
		if err != nil {
			t.Fatalf("Create() error = %v, want nil", err)
		}
		h := newMuxHandler(m.Handler())

		req := httptest.NewRequest(http.MethodGet, "/r/"+l.Code, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusFound {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusFound)
		}
		if got, want := rec.Header().Get("Location"), "https://fallback.example.com/"; got != want {
			t.Fatalf("Location = %q, want %q", got, want)
		}
	})

	t.Run("without fallback 404", func(t *testing.T) {
		t.Parallel()
		m := newTestManager(t, smartlink.WithResolver(resolver))
		l, err := m.Create(context.Background(), smartlink.CreateParams{Ref: "paused-2", SkipRefCheck: true})
		if err != nil {
			t.Fatalf("Create() error = %v, want nil", err)
		}
		h := newMuxHandler(m.Handler())

		req := httptest.NewRequest(http.MethodGet, "/r/"+l.Code, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
		}
	})
}

// TestHandlerRefNotFound asserts a Resolver error wrapping ErrRefNotFound —
// the ref names no known Spec anymore — is dead-link handling like
// ErrNoTarget, not a 500: a hard-deleted offer must not turn every click
// into an internal error.
func TestHandlerRefNotFound(t *testing.T) {
	t.Parallel()
	resolver := func(context.Context, smartlink.Link) (smartlink.Decider, error) {
		return nil, fmt.Errorf("offer lookup: %w", smartlink.ErrRefNotFound)
	}
	m := newTestManager(t, smartlink.WithResolver(resolver), smartlink.WithFallbackURL("https://fallback.example.com/"))
	l, err := m.Create(context.Background(), smartlink.CreateParams{Ref: "gone-1", SkipRefCheck: true})
	if err != nil {
		t.Fatalf("Create() error = %v, want nil", err)
	}
	h := newMuxHandler(m.Handler())

	req := httptest.NewRequest(http.MethodGet, "/r/"+l.Code, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d (ErrRefNotFound is a dead link, not an outage)", rec.Code, http.StatusFound)
	}
	if got, want := rec.Header().Get("Location"), "https://fallback.example.com/"; got != want {
		t.Fatalf("Location = %q, want %q", got, want)
	}
}

// TestHandlerImpossibleCodeSkipsStore asserts a code Create could never have
// minted — over 64 chars or outside [A-Za-z0-9_-] — answers as a dead link
// without hitting the Store.
func TestHandlerImpossibleCodeSkipsStore(t *testing.T) {
	t.Parallel()
	store := &countingStore{Store: smartlink.NewMemoryStore()}
	m, err := smartlink.NewManager(store)
	if err != nil {
		t.Fatalf("NewManager() error = %v, want nil", err)
	}
	h := newMuxHandler(m.Handler())

	for _, code := range []string{"has%20space", "caf%C3%A9", strings.Repeat("x", 65)} {
		req := httptest.NewRequest(http.MethodGet, "/r/"+code, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("code %q: status = %d, want %d", code, rec.Code, http.StatusNotFound)
		}
	}
	if got := store.gets.Load(); got != 0 {
		t.Fatalf("Store.Get calls = %d, want 0 (impossible codes must not reach the Store)", got)
	}
}

// TestHandlerDoesNotMutateVisitFuncParams asserts the metadata merge builds a
// fresh map instead of writing into whatever map the consumer's VisitFunc
// returned — a consumer handing over a shared or reused map must not see it
// change behind its back.
func TestHandlerDoesNotMutateVisitFuncParams(t *testing.T) {
	t.Parallel()
	shared := map[string]string{"sub": "mine"}
	visitFn := func(_ *http.Request, v smartlink.Visit) smartlink.Visit {
		v.Params = shared
		return v
	}
	m := newTestManager(t, smartlink.WithVisitFunc(visitFn))
	l, err := m.Create(context.Background(), smartlink.CreateParams{
		Target:   "https://dest.example.com/",
		Metadata: map[string]string{"aff": "abc"},
	})
	if err != nil {
		t.Fatalf("Create() error = %v, want nil", err)
	}
	h := newMuxHandler(m.Handler())

	req := httptest.NewRequest(http.MethodGet, "/r/"+l.Code, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusFound)
	}
	if len(shared) != 1 || shared["sub"] != "mine" {
		t.Fatalf("VisitFunc's map was mutated by the handler: %v", shared)
	}
}

// TestHandlerResolverNilDecider asserts a resolver that returns (nil, nil)
// at serve time (its row admitted via SkipRefCheck) answers 500 instead of
// panicking on Decide.
func TestHandlerResolverNilDecider(t *testing.T) {
	t.Parallel()
	resolver := func(context.Context, smartlink.Link) (smartlink.Decider, error) { return nil, nil }
	m := newTestManager(t, smartlink.WithResolver(resolver))
	l, err := m.Create(context.Background(), smartlink.CreateParams{Ref: "offer-1", SkipRefCheck: true})
	if err != nil {
		t.Fatalf("Create() error = %v, want nil", err)
	}
	h := newMuxHandler(m.Handler())

	req := httptest.NewRequest(http.MethodGet, "/r/"+l.Code, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d (nil Decider is a consumer bug, not a dead link)", rec.Code, http.StatusInternalServerError)
	}
}

// TestHandlerDecoratorsWrapBothKinds asserts the WithDecorators chain runs
// for Target- and Ref-backed links alike: a diverting decorator rewrites the
// redirect destination for both.
func TestHandlerDecoratorsWrapBothKinds(t *testing.T) {
	t.Parallel()
	const guardURL = "https://guard.example.com/"
	divert := func(next smartlink.Decider) smartlink.Decider {
		return smartlink.DecideFunc(func(v smartlink.Visit) smartlink.Decision {
			d := next.Decide(v)
			d.URL = guardURL
			return d
		})
	}
	m := newTestManager(t, smartlink.WithResolver(geoResolver()), smartlink.WithDecorators(divert))
	ctx := context.Background()

	target, err := m.Create(ctx, smartlink.CreateParams{Target: "https://dest.example.com/"})
	if err != nil {
		t.Fatalf("Create(target) error = %v, want nil", err)
	}
	ref, err := m.Create(ctx, smartlink.CreateParams{Ref: "offer-1"})
	if err != nil {
		t.Fatalf("Create(ref) error = %v, want nil", err)
	}
	h := newMuxHandler(m.Handler())

	for name, code := range map[string]string{"target": target.Code, "ref": ref.Code} {
		req := httptest.NewRequest(http.MethodGet, "/r/"+code, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if got := rec.Header().Get("Location"); got != guardURL {
			t.Fatalf("%s link Location = %q, want %q (decorator must wrap every link's Decider)", name, got, guardURL)
		}
	}
}

// TestHandlerRefNoResolver asserts a Ref-backed Link with no configured
// Resolver is a configuration error: 500, not a dead link.
func TestHandlerRefNoResolver(t *testing.T) {
	t.Parallel()
	m := newTestManager(t)
	l, err := m.Create(context.Background(), smartlink.CreateParams{Ref: "any-ref"})
	if err != nil {
		t.Fatalf("Create() error = %v, want nil", err)
	}
	h := newMuxHandler(m.Handler())

	req := httptest.NewRequest(http.MethodGet, "/r/"+l.Code, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

// TestHandlerDeadLink covers the three dead-link sentinels (unknown,
// expired, deactivated) both with and without a configured fallback URL.
func TestHandlerDeadLink(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		codeFor func(t *testing.T, m *smartlink.Manager) string
	}{
		{
			name:    "unknown",
			codeFor: func(*testing.T, *smartlink.Manager) string { return "does-not-exist" },
		},
		{
			name: "expired",
			codeFor: func(t *testing.T, m *smartlink.Manager) string {
				l, err := m.Create(context.Background(), smartlink.CreateParams{
					Target:    "https://dest.example.com/",
					ExpiresAt: time.Now().Add(-time.Hour),
				})
				if err != nil {
					t.Fatalf("Create() error = %v, want nil", err)
				}
				return l.Code
			},
		},
		{
			name: "deactivated",
			codeFor: func(t *testing.T, m *smartlink.Manager) string {
				l, err := m.Create(context.Background(), smartlink.CreateParams{Target: "https://dest.example.com/"})
				if err != nil {
					t.Fatalf("Create() error = %v, want nil", err)
				}
				if err := m.Deactivate(context.Background(), l.Code); err != nil {
					t.Fatalf("Deactivate() error = %v, want nil", err)
				}
				return l.Code
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name+"/fallback", func(t *testing.T) {
			t.Parallel()
			m := newTestManager(t, smartlink.WithFallbackURL("https://fallback.example.com/"))
			code := tc.codeFor(t, m)
			h := newMuxHandler(m.Handler())

			req := httptest.NewRequest(http.MethodGet, "/r/"+code, nil)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != http.StatusFound {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusFound)
			}
			if got, want := rec.Header().Get("Location"), "https://fallback.example.com/"; got != want {
				t.Fatalf("Location = %q, want %q", got, want)
			}
		})

		t.Run(tc.name+"/404", func(t *testing.T) {
			t.Parallel()
			m := newTestManager(t)
			code := tc.codeFor(t, m)
			h := newMuxHandler(m.Handler())

			req := httptest.NewRequest(http.MethodGet, "/r/"+code, nil)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
			}
		})
	}
}

var errStoreBoom = errors.New("store boundary: unavailable")

// erroringStore is a Store whose every method errors, to simulate a backing
// database outage.
type erroringStore struct{}

func (erroringStore) Create(context.Context, smartlink.Link) error { return errStoreBoom }
func (erroringStore) Get(context.Context, string) (smartlink.Link, error) {
	return smartlink.Link{}, errStoreBoom
}
func (erroringStore) List(context.Context, smartlink.Filter) ([]smartlink.Link, error) {
	return nil, errStoreBoom
}
func (erroringStore) Deactivate(context.Context, string, string, time.Time) error {
	return errStoreBoom
}
func (erroringStore) Activate(context.Context, string, string) error { return errStoreBoom }
func (erroringStore) Delete(context.Context, string, string) error   { return errStoreBoom }

// TestHandlerStoreOutage asserts a Store error other than the dead-link
// sentinels is a 500 — an outage must read as an outage, not as every link
// being gone — and that no-store still applies.
func TestHandlerStoreOutage(t *testing.T) {
	t.Parallel()
	m, err := smartlink.NewManager(erroringStore{})
	if err != nil {
		t.Fatalf("NewManager() error = %v, want nil", err)
	}
	h := newMuxHandler(m.Handler())

	req := httptest.NewRequest(http.MethodGet, "/r/any-code", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want %q even on outage", got, "no-store")
	}
}

// TestHandlerOnHit asserts OnHit fires synchronously, exactly once, only on
// a successful redirect, carrying the resolved Link (ShortURL populated),
// the merged Visit, and the final Decision.
func TestHandlerOnHit(t *testing.T) {
	t.Parallel()

	t.Run("fires on successful redirect", func(t *testing.T) {
		t.Parallel()
		var got smartlink.Hit
		hits := 0
		onHit := func(_ context.Context, h smartlink.Hit) {
			hits++
			got = h
		}
		m := newTestManager(t, smartlink.WithOnHit(onHit), smartlink.WithBaseURL("https://s.example.com"))
		l, err := m.Create(context.Background(), smartlink.CreateParams{
			Target:   "https://dest.example.com/",
			Metadata: map[string]string{"m1": "v1"},
		})
		if err != nil {
			t.Fatalf("Create() error = %v, want nil", err)
		}
		h := newMuxHandler(m.Handler())

		req := httptest.NewRequest(http.MethodGet, "/r/"+l.Code+"?q=1", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if hits != 1 {
			t.Fatalf("onHit calls = %d, want 1", hits)
		}
		if got.Link.Code != l.Code || got.Link.ShortURL == "" {
			t.Fatalf("Hit.Link = %+v, want Code %q and ShortURL populated", got.Link, l.Code)
		}
		if got.Visit.Params["q"] != "1" || got.Visit.Params["m1"] != "v1" {
			t.Fatalf("Hit.Visit.Params = %+v, want merged query %q and metadata %q", got.Visit.Params, "q=1", "m1=v1")
		}
		if got.Decision.URL != rec.Header().Get("Location") {
			t.Fatalf("Hit.Decision.URL = %q, want match redirect Location %q", got.Decision.URL, rec.Header().Get("Location"))
		}
	})

	t.Run("not called on dead link", func(t *testing.T) {
		t.Parallel()
		hits := 0
		m := newTestManager(t, smartlink.WithOnHit(func(context.Context, smartlink.Hit) { hits++ }))
		h := newMuxHandler(m.Handler())

		req := httptest.NewRequest(http.MethodGet, "/r/unknown-code", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
		}
		if hits != 0 {
			t.Fatalf("onHit calls = %d, want 0 on dead link", hits)
		}
	})
}

// TestHandler307 asserts WithRedirectStatus(307) is honored by the redirect.
func TestHandler307(t *testing.T) {
	t.Parallel()
	m := newTestManager(t, smartlink.WithRedirectStatus(http.StatusTemporaryRedirect))
	l, err := m.Create(context.Background(), smartlink.CreateParams{Target: "https://dest.example.com/"})
	if err != nil {
		t.Fatalf("Create() error = %v, want nil", err)
	}
	h := newMuxHandler(m.Handler())

	req := httptest.NewRequest(http.MethodGet, "/r/"+l.Code, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusTemporaryRedirect {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusTemporaryRedirect)
	}
}

// TestHandlerPathFallback asserts a Handler mounted without a {code}
// pattern (so r.PathValue("code") is empty) falls back to extracting the
// code from the trimmed URL path.
func TestHandlerPathFallback(t *testing.T) {
	t.Parallel()
	m := newTestManager(t)
	l, err := m.Create(context.Background(), smartlink.CreateParams{Target: "https://dest.example.com/", Code: "fallback-code"})
	if err != nil {
		t.Fatalf("Create() error = %v, want nil", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/"+l.Code, nil)
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusFound)
	}
	if got, want := rec.Header().Get("Location"), "https://dest.example.com/"; got != want {
		t.Fatalf("Location = %q, want %q", got, want)
	}
}

// TestHandlerOnHitContextDetached asserts the OnHit callback receives a
// cancellation-detached context: a client disconnecting right after the
// redirect (canceling the request context) must not cancel hit delivery.
func TestHandlerOnHitContextDetached(t *testing.T) {
	t.Parallel()
	hitCtxErr := make(chan error, 1)
	m := newTestManager(t, smartlink.WithOnHit(func(ctx context.Context, _ smartlink.Hit) {
		hitCtxErr <- ctx.Err()
	}))
	l, err := m.Create(context.Background(), smartlink.CreateParams{Target: "https://dest.example.com/"})
	if err != nil {
		t.Fatalf("Create() error = %v, want nil", err)
	}
	h := newMuxHandler(m.Handler())

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // the client is already gone
	req := httptest.NewRequest(http.MethodGet, "/r/"+l.Code, nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusFound)
	}
	if err := <-hitCtxErr; err != nil {
		t.Fatalf("OnHit ctx.Err() = %v, want nil (must be cancellation-detached)", err)
	}
}

// TestHandlerBlocksDisallowedSchemeFromStore asserts a Target row written to
// the Store outside the Manager (bypassing Create's validation) cannot serve
// a disallowed scheme as a redirect: the hit answers 500, not a
// javascript: Location.
func TestHandlerBlocksDisallowedSchemeFromStore(t *testing.T) {
	t.Parallel()
	store := smartlink.NewMemoryStore()
	ctx := context.Background()
	if err := store.Create(ctx, smartlink.Link{Code: "evil", Target: "javascript:alert(1)", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("store Create() error = %v, want nil", err)
	}
	m, err := smartlink.NewManager(store)
	if err != nil {
		t.Fatalf("NewManager() error = %v, want nil", err)
	}
	h := newMuxHandler(m.Handler())

	req := httptest.NewRequest(http.MethodGet, "/r/evil", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d (disallowed scheme must not redirect)", rec.Code, http.StatusInternalServerError)
	}
	if loc := rec.Header().Get("Location"); loc != "" {
		t.Fatalf("Location = %q, want empty", loc)
	}
}
