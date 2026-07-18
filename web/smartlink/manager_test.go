package smartlink_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/core/id"
	"github.com/dmitrymomot/forge/resilience/cache"
	"github.com/dmitrymomot/forge/web/smartlink"
)

func newTestManager(t *testing.T, opts ...smartlink.ManagerOption) *smartlink.Manager {
	t.Helper()
	m, err := smartlink.NewManager(smartlink.NewMemoryStore(), opts...)
	if err != nil {
		t.Fatalf("NewManager() error = %v, want nil", err)
	}
	return m
}

// TestNewManagerRejects301 asserts WithRedirectStatus only accepts 302 and
// 307 — a 301 (or any other status) is a NewManager error.
func TestNewManagerRejects301(t *testing.T) {
	t.Parallel()
	cases := []int{301, 200, 404, 0}
	for _, code := range cases {
		if _, err := smartlink.NewManager(smartlink.NewMemoryStore(), smartlink.WithRedirectStatus(code)); err == nil {
			t.Fatalf("NewManager(WithRedirectStatus(%d)) error = nil, want error", code)
		}
	}
	if _, err := smartlink.NewManager(smartlink.NewMemoryStore(), smartlink.WithRedirectStatus(302)); err != nil {
		t.Fatalf("NewManager(WithRedirectStatus(302)) error = %v, want nil", err)
	}
	if _, err := smartlink.NewManager(smartlink.NewMemoryStore(), smartlink.WithRedirectStatus(307)); err != nil {
		t.Fatalf("NewManager(WithRedirectStatus(307)) error = %v, want nil", err)
	}
}

// TestNewManagerBadBaseURL asserts WithBaseURL requires an absolute http(s) URL.
func TestNewManagerBadBaseURL(t *testing.T) {
	t.Parallel()
	cases := []string{"", "app.example.com/verify", "ftp://example.com/"}
	for _, u := range cases {
		if _, err := smartlink.NewManager(smartlink.NewMemoryStore(), smartlink.WithBaseURL(u)); err == nil {
			t.Fatalf("NewManager(WithBaseURL(%q)) error = nil, want error", u)
		}
	}
	if _, err := smartlink.NewManager(smartlink.NewMemoryStore(), smartlink.WithBaseURL("https://s.example.com")); err != nil {
		t.Fatalf("NewManager(WithBaseURL(valid)) error = %v, want nil", err)
	}
}

// TestNewManagerBadFallbackURL asserts WithFallbackURL requires an absolute
// http(s) URL, same as WithBaseURL: a relative value would silently redirect
// relative to the handler's own path instead of failing construction.
func TestNewManagerBadFallbackURL(t *testing.T) {
	t.Parallel()
	cases := []string{"", "/relative/path", "app.example.com/verify", "ftp://example.com/"}
	for _, u := range cases {
		if _, err := smartlink.NewManager(smartlink.NewMemoryStore(), smartlink.WithFallbackURL(u)); err == nil {
			t.Fatalf("NewManager(WithFallbackURL(%q)) error = nil, want error", u)
		}
	}
	if _, err := smartlink.NewManager(smartlink.NewMemoryStore(), smartlink.WithFallbackURL("https://fallback.example.com/")); err != nil {
		t.Fatalf("NewManager(WithFallbackURL(valid)) error = %v, want nil", err)
	}
}

// TestNewManagerRejectsNonPositiveCacheTTL asserts WithCache rejects a
// zero or negative ttl: [cache.WithTTL] treats it as "never expire", which
// would defeat the bounded-staleness guarantee on a failed cache eviction.
func TestNewManagerRejectsNonPositiveCacheTTL(t *testing.T) {
	t.Parallel()
	for _, ttl := range []time.Duration{0, -time.Second} {
		if _, err := smartlink.NewManager(smartlink.NewMemoryStore(), smartlink.WithCache(cache.NewMemoryStore(), ttl)); err == nil {
			t.Fatalf("NewManager(WithCache(ttl=%s)) error = nil, want error", ttl)
		}
	}
	if _, err := smartlink.NewManager(smartlink.NewMemoryStore(), smartlink.WithCache(cache.NewMemoryStore(), time.Minute)); err != nil {
		t.Fatalf("NewManager(WithCache(ttl=1m)) error = %v, want nil", err)
	}
}

// TestCreateTargetXorRef asserts Create requires exactly one of Target/Ref.
func TestCreateTargetXorRef(t *testing.T) {
	t.Parallel()
	m := newTestManager(t)
	ctx := context.Background()

	if _, err := m.Create(ctx, smartlink.CreateParams{}); !errors.Is(err, smartlink.ErrInvalidLink) {
		t.Fatalf("Create(neither) = %v, want ErrInvalidLink", err)
	}
	if _, err := m.Create(ctx, smartlink.CreateParams{Target: "https://example.com/", Ref: "offer-1"}); !errors.Is(err, smartlink.ErrInvalidLink) {
		t.Fatalf("Create(both) = %v, want ErrInvalidLink", err)
	}
}

// TestCreateGeneratedCodeDefault asserts the default code function produces a
// 16-character lowercase Crockford base32 code parseable via id.ParseShort.
func TestCreateGeneratedCodeDefault(t *testing.T) {
	t.Parallel()
	m := newTestManager(t)
	l, err := m.Create(context.Background(), smartlink.CreateParams{Target: "https://example.com/"})
	if err != nil {
		t.Fatalf("Create() error = %v, want nil", err)
	}
	if len(l.Code) != 16 {
		t.Fatalf("len(Code) = %d, want 16 (Code = %q)", len(l.Code), l.Code)
	}
	const crockfordLower = "0123456789abcdefghjkmnpqrstvwxyz"
	for _, c := range l.Code {
		if !strings.ContainsRune(crockfordLower, c) {
			t.Fatalf("Code %q has char %q outside lowercase Crockford base32", l.Code, c)
		}
	}
	if _, err := id.ParseShort(l.Code); err != nil {
		t.Fatalf("id.ParseShort(%q) error = %v, want nil", l.Code, err)
	}
}

// TestCreateCollisionRetry asserts Create retries a colliding generated code
// up to 5 times, and gives up with an error when every attempt collides.
func TestCreateCollisionRetry(t *testing.T) {
	t.Parallel()

	t.Run("succeeds after two collisions", func(t *testing.T) {
		t.Parallel()
		store := smartlink.NewMemoryStore()
		ctx := context.Background()
		for _, taken := range []string{"taken1", "taken2"} {
			if err := store.Create(ctx, smartlink.Link{Code: taken, Target: "https://x.example.com/"}); err != nil {
				t.Fatalf("seed Create(%q) = %v, want nil", taken, err)
			}
		}
		codes := []string{"taken1", "taken2", "fresh"}
		i := 0
		codeFunc := func() string {
			c := codes[i]
			i++
			return c
		}
		m, err := smartlink.NewManager(store, smartlink.WithCodeFunc(codeFunc))
		if err != nil {
			t.Fatalf("NewManager() error = %v, want nil", err)
		}
		got, cerr := m.Create(ctx, smartlink.CreateParams{Target: "https://example.com/"})
		if cerr != nil {
			t.Fatalf("Create() error = %v, want nil", cerr)
		}
		if got.Code != "fresh" {
			t.Fatalf("Code = %q, want %q", got.Code, "fresh")
		}
	})

	t.Run("always colliding gives up", func(t *testing.T) {
		t.Parallel()
		store := smartlink.NewMemoryStore()
		ctx := context.Background()
		if err := store.Create(ctx, smartlink.Link{Code: "dup", Target: "https://x.example.com/"}); err != nil {
			t.Fatalf("seed Create() = %v, want nil", err)
		}
		m, err := smartlink.NewManager(store, smartlink.WithCodeFunc(func() string { return "dup" }))
		if err != nil {
			t.Fatalf("NewManager() error = %v, want nil", err)
		}
		if _, err := m.Create(ctx, smartlink.CreateParams{Target: "https://example.com/"}); err == nil {
			t.Fatal("Create() error = nil, want error after exhausting retries")
		}
	})
}

// TestCreateGeneratedCodeEmptyFails asserts a [WithCodeFunc] that returns ""
// never persists a Link with an empty code: each empty result is treated as
// a failed attempt, and Create errors once the retry budget is exhausted.
func TestCreateGeneratedCodeEmptyFails(t *testing.T) {
	t.Parallel()
	m := newTestManager(t, smartlink.WithCodeFunc(func() string { return "" }))
	if _, err := m.Create(context.Background(), smartlink.CreateParams{Target: "https://example.com/"}); err == nil {
		t.Fatal("Create() error = nil, want error (empty generated code must not be stored)")
	}
}

// TestCreateVanity covers caller-supplied Code validation: charset, length,
// reserved blocklist, and duplicate surfacing.
func TestCreateVanity(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("valid", func(t *testing.T) {
		t.Parallel()
		m := newTestManager(t)
		l, err := m.Create(ctx, smartlink.CreateParams{Target: "https://example.com/", Code: "my-code_1"})
		if err != nil {
			t.Fatalf("Create() error = %v, want nil", err)
		}
		if l.Code != "my-code_1" {
			t.Fatalf("Code = %q, want %q", l.Code, "my-code_1")
		}
	})

	t.Run("invalid chars", func(t *testing.T) {
		t.Parallel()
		m := newTestManager(t)
		if _, err := m.Create(ctx, smartlink.CreateParams{Target: "https://example.com/", Code: "bad code!"}); !errors.Is(err, smartlink.ErrInvalidLink) {
			t.Fatalf("Create() = %v, want ErrInvalidLink", err)
		}
	})

	t.Run("too long", func(t *testing.T) {
		t.Parallel()
		m := newTestManager(t)
		long := strings.Repeat("a", 65)
		if _, err := m.Create(ctx, smartlink.CreateParams{Target: "https://example.com/", Code: long}); !errors.Is(err, smartlink.ErrInvalidLink) {
			t.Fatalf("Create() = %v, want ErrInvalidLink", err)
		}
	})

	t.Run("reserved", func(t *testing.T) {
		t.Parallel()
		m := newTestManager(t)
		if _, err := m.Create(ctx, smartlink.CreateParams{Target: "https://example.com/", Code: "api"}); !errors.Is(err, smartlink.ErrCodeReserved) {
			t.Fatalf("Create() = %v, want ErrCodeReserved", err)
		}
	})

	t.Run("duplicate", func(t *testing.T) {
		t.Parallel()
		m := newTestManager(t)
		if _, err := m.Create(ctx, smartlink.CreateParams{Target: "https://example.com/", Code: "dup"}); err != nil {
			t.Fatalf("Create() error = %v, want nil", err)
		}
		if _, err := m.Create(ctx, smartlink.CreateParams{Target: "https://b.example.com/", Code: "dup"}); !errors.Is(err, smartlink.ErrDuplicate) {
			t.Fatalf("Create() = %v, want ErrDuplicate", err)
		}
	})
}

// TestCreateTargetValidation covers Target compile/scheme/host validation.
func TestCreateTargetValidation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cases := []struct {
		name    string
		target  string
		wantErr bool
	}{
		{"bad template", "https://example.com/{unknown}", true},
		{"disallowed scheme", "ftp://example.com/file", true},
		{"scheme-relative", "//example.com/path", true},
		{"macro host allowed", "https://{param.host}/path", false},
		{"plain host required", "https:///path", true},
		{"valid absolute", "https://example.com/path", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := newTestManager(t)
			_, err := m.Create(ctx, smartlink.CreateParams{Target: tc.target})
			if tc.wantErr && !errors.Is(err, smartlink.ErrInvalidLink) {
				t.Fatalf("Create(%q) = %v, want ErrInvalidLink", tc.target, err)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("Create(%q) = %v, want nil", tc.target, err)
			}
		})
	}
}

// TestCreateRefValidation covers Ref validation via the configured Resolver.
func TestCreateRefValidation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("unknown ref wraps ErrInvalidLink", func(t *testing.T) {
		t.Parallel()
		resolverErr := fmt.Errorf("offer lookup: %w", smartlink.ErrRefNotFound)
		resolver := func(context.Context, smartlink.Link) (smartlink.Decider, error) {
			return nil, resolverErr
		}
		m := newTestManager(t, smartlink.WithResolver(resolver))
		_, err := m.Create(ctx, smartlink.CreateParams{Ref: "missing-offer"})
		if !errors.Is(err, smartlink.ErrInvalidLink) {
			t.Fatalf("Create() = %v, want ErrInvalidLink", err)
		}
		if !errors.Is(err, resolverErr) {
			t.Fatalf("Create() = %v, want wrapped resolver error", err)
		}
	})

	t.Run("resolver infrastructure error is not ErrInvalidLink", func(t *testing.T) {
		t.Parallel()
		infraErr := errors.New("consumer db timeout")
		resolver := func(context.Context, smartlink.Link) (smartlink.Decider, error) {
			return nil, infraErr
		}
		m := newTestManager(t, smartlink.WithResolver(resolver))
		_, err := m.Create(ctx, smartlink.CreateParams{Ref: "offer-1"})
		if errors.Is(err, smartlink.ErrInvalidLink) {
			t.Fatalf("Create() = %v, must not read as caller input error", err)
		}
		if !errors.Is(err, infraErr) {
			t.Fatalf("Create() = %v, want wrapped infrastructure error", err)
		}
	})

	t.Run("SkipRefCheck bypasses resolver", func(t *testing.T) {
		t.Parallel()
		called := false
		resolver := func(context.Context, smartlink.Link) (smartlink.Decider, error) {
			called = true
			return nil, errors.New("should not be called")
		}
		m := newTestManager(t, smartlink.WithResolver(resolver))
		if _, err := m.Create(ctx, smartlink.CreateParams{Ref: "future-offer", SkipRefCheck: true}); err != nil {
			t.Fatalf("Create() error = %v, want nil", err)
		}
		if called {
			t.Fatal("resolver was called despite SkipRefCheck")
		}
	})

	t.Run("no resolver configured skips check", func(t *testing.T) {
		t.Parallel()
		m := newTestManager(t)
		if _, err := m.Create(ctx, smartlink.CreateParams{Ref: "any-ref"}); err != nil {
			t.Fatalf("Create() error = %v, want nil", err)
		}
	})
}

// TestCreateMetadataCloned asserts CreateParams.Metadata is cloned: mutating
// the caller's map after Create must not affect the returned or stored Link.
func TestCreateMetadataCloned(t *testing.T) {
	t.Parallel()
	m := newTestManager(t)
	ctx := context.Background()
	meta := map[string]string{"k": "v1"}
	l, err := m.Create(ctx, smartlink.CreateParams{Target: "https://example.com/", Code: "meta1", Metadata: meta})
	if err != nil {
		t.Fatalf("Create() error = %v, want nil", err)
	}
	meta["k"] = "mutated"
	if l.Metadata["k"] != "v1" {
		t.Fatalf("Metadata[k] = %q, want %q (caller mutation leaked into returned Link)", l.Metadata["k"], "v1")
	}
	got, err := m.Get(ctx, "meta1")
	if err != nil {
		t.Fatalf("Get() error = %v, want nil", err)
	}
	if got.Metadata["k"] != "v1" {
		t.Fatalf("stored Metadata[k] = %q, want %q", got.Metadata["k"], "v1")
	}
}

// TestShortURL covers ShortURL with/without WithBaseURL and slash normalization.
func TestShortURL(t *testing.T) {
	t.Parallel()

	t.Run("without base returns empty", func(t *testing.T) {
		t.Parallel()
		m := newTestManager(t)
		if got := m.ShortURL("abc"); got != "" {
			t.Fatalf("ShortURL() = %q, want empty", got)
		}
	})

	t.Run("with base, no trailing slash", func(t *testing.T) {
		t.Parallel()
		m := newTestManager(t, smartlink.WithBaseURL("https://s.example.com"))
		if got, want := m.ShortURL("abc"), "https://s.example.com/abc"; got != want {
			t.Fatalf("ShortURL() = %q, want %q", got, want)
		}
	})

	t.Run("with base, trailing slash", func(t *testing.T) {
		t.Parallel()
		m := newTestManager(t, smartlink.WithBaseURL("https://s.example.com/"))
		if got, want := m.ShortURL("abc"), "https://s.example.com/abc"; got != want {
			t.Fatalf("ShortURL() = %q, want %q", got, want)
		}
	})

	t.Run("with base, multiple trailing slashes", func(t *testing.T) {
		t.Parallel()
		m := newTestManager(t, smartlink.WithBaseURL("https://s.example.com//"))
		if got, want := m.ShortURL("abc"), "https://s.example.com/abc"; got != want {
			t.Fatalf("ShortURL() = %q, want %q (must not double up the slash)", got, want)
		}
	})

	t.Run("populated on Create result", func(t *testing.T) {
		t.Parallel()
		m := newTestManager(t, smartlink.WithBaseURL("https://s.example.com"))
		l, err := m.Create(context.Background(), smartlink.CreateParams{Target: "https://example.com/", Code: "shorty"})
		if err != nil {
			t.Fatalf("Create() error = %v, want nil", err)
		}
		if want := "https://s.example.com/shorty"; l.ShortURL != want {
			t.Fatalf("ShortURL = %q, want %q", l.ShortURL, want)
		}
	})
}

// TestCreateShortURLNotPersisted asserts ShortURL is derived, never
// persisted: the stored record (read directly from the Store) must have an
// empty ShortURL while the Create-returned copy has it populated.
func TestCreateShortURLNotPersisted(t *testing.T) {
	t.Parallel()
	store := smartlink.NewMemoryStore()
	m, err := smartlink.NewManager(store, smartlink.WithBaseURL("https://s.example.com"))
	if err != nil {
		t.Fatalf("NewManager() error = %v, want nil", err)
	}
	ctx := context.Background()
	l, err := m.Create(ctx, smartlink.CreateParams{Target: "https://example.com/", Code: "persist1"})
	if err != nil {
		t.Fatalf("Create() error = %v, want nil", err)
	}
	if l.ShortURL == "" {
		t.Fatal("returned Link.ShortURL is empty, want populated")
	}
	stored, err := store.Get(ctx, "persist1")
	if err != nil {
		t.Fatalf("store.Get() error = %v, want nil", err)
	}
	if stored.ShortURL != "" {
		t.Fatalf("stored Link.ShortURL = %q, want empty (must not be persisted)", stored.ShortURL)
	}
}

// TestLifecycle asserts Deactivate/Activate/Delete reach the Store.
func TestLifecycle(t *testing.T) {
	t.Parallel()
	m := newTestManager(t)
	ctx := context.Background()
	l, err := m.Create(ctx, smartlink.CreateParams{Target: "https://example.com/", Code: "life1"})
	if err != nil {
		t.Fatalf("Create() error = %v, want nil", err)
	}

	if err := m.Deactivate(ctx, l.Code); err != nil {
		t.Fatalf("Deactivate() error = %v, want nil", err)
	}
	got, err := m.Get(ctx, l.Code)
	if err != nil {
		t.Fatalf("Get() error = %v, want nil", err)
	}
	if got.DeactivatedAt.IsZero() {
		t.Fatal("DeactivatedAt is zero after Deactivate, want set")
	}

	if err := m.Activate(ctx, l.Code); err != nil {
		t.Fatalf("Activate() error = %v, want nil", err)
	}
	got, err = m.Get(ctx, l.Code)
	if err != nil {
		t.Fatalf("Get() error = %v, want nil", err)
	}
	if !got.DeactivatedAt.IsZero() {
		t.Fatal("DeactivatedAt is non-zero after Activate, want zero")
	}

	if err := m.Delete(ctx, l.Code); err != nil {
		t.Fatalf("Delete() error = %v, want nil", err)
	}
	if _, err := m.Get(ctx, l.Code); !errors.Is(err, smartlink.ErrNotFound) {
		t.Fatalf("Get() after Delete = %v, want ErrNotFound", err)
	}
}

// TestRandomCode asserts RandomCode(n) generates n-character codes drawn from
// the base58 alphabet.
func TestRandomCode(t *testing.T) {
	t.Parallel()
	const alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"
	gen := smartlink.RandomCode(12)
	for range 20 {
		code := gen()
		if len(code) != 12 {
			t.Fatalf("len(code) = %d, want 12 (code = %q)", len(code), code)
		}
		for _, c := range code {
			if !strings.ContainsRune(alphabet, c) {
				t.Fatalf("code %q contains char %q outside base58 alphabet", code, c)
			}
		}
	}
}

// TestNewManagerRejectsUnknownParamPolicy asserts an out-of-range
// WithLinkParamPolicy fails at construction like every other validated
// option, instead of surfacing per-Create and per-hit.
func TestNewManagerRejectsUnknownParamPolicy(t *testing.T) {
	t.Parallel()
	if _, err := smartlink.NewManager(smartlink.NewMemoryStore(), smartlink.WithLinkParamPolicy(smartlink.ParamPolicy(7))); err == nil {
		t.Fatal("NewManager(WithLinkParamPolicy(7)) error = nil, want construction error")
	}
}

// TestCreateGeneratedCodeValidation asserts generated codes face the same
// reserved-word and charset rules as vanity codes.
func TestCreateGeneratedCodeValidation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("reserved generated code retried", func(t *testing.T) {
		t.Parallel()
		seq := []string{"api", "okcode1"}
		i := 0
		m := newTestManager(t, smartlink.WithCodeFunc(func() string { i++; return seq[i-1] }))
		l, err := m.Create(ctx, smartlink.CreateParams{Target: "https://example.com/"})
		if err != nil {
			t.Fatalf("Create() error = %v, want nil", err)
		}
		if l.Code != "okcode1" {
			t.Fatalf("Create().Code = %q, want the non-reserved retry %q", l.Code, "okcode1")
		}
	})

	t.Run("always-reserved generator exhausts attempts", func(t *testing.T) {
		t.Parallel()
		m := newTestManager(t, smartlink.WithCodeFunc(func() string { return "api" }))
		_, err := m.Create(ctx, smartlink.CreateParams{Target: "https://example.com/"})
		if !errors.Is(err, smartlink.ErrCodeReserved) {
			t.Fatalf("Create() = %v, want wrapped ErrCodeReserved", err)
		}
	})

	t.Run("invalid charset fails fast", func(t *testing.T) {
		t.Parallel()
		calls := 0
		m := newTestManager(t, smartlink.WithCodeFunc(func() string { calls++; return "a/b" }))
		_, err := m.Create(ctx, smartlink.CreateParams{Target: "https://example.com/"})
		if !errors.Is(err, smartlink.ErrInvalidLink) {
			t.Fatalf("Create() = %v, want ErrInvalidLink", err)
		}
		if calls != 1 {
			t.Fatalf("code generator calls = %d, want 1 (broken generator must not be retried)", calls)
		}
	})
}
