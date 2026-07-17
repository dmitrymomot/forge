package shortlink_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/web/shortlink"
)

func TestNew_Panics(t *testing.T) {
	t.Parallel()
	assert.Panics(t, func() { shortlink.New(nil) })
	assert.Panics(t, func() { shortlink.New(shortlink.NewMemoryStore(), shortlink.WithCodeLength(3)) })
	assert.Panics(t, func() { shortlink.New(shortlink.NewMemoryStore(), shortlink.WithCodeLength(33)) })
	assert.Panics(t, func() { shortlink.New(shortlink.NewMemoryStore(), shortlink.WithCacheTTL(0)) })
	assert.Panics(t, func() { shortlink.New(shortlink.NewMemoryStore(), shortlink.WithSchemes()) })
	assert.Panics(t, func() { shortlink.New(shortlink.NewMemoryStore(), shortlink.WithRedirectStatus(301)) })
	assert.NotPanics(t, func() { shortlink.New(shortlink.NewMemoryStore(), shortlink.WithRedirectStatus(307)) })
}

func TestCreate_GeneratedCode(t *testing.T) {
	t.Parallel()
	mgr := shortlink.New(shortlink.NewMemoryStore())
	l, err := mgr.Create(context.Background(), shortlink.CreateParams{URL: "https://example.com/page"})
	require.NoError(t, err)
	assert.Len(t, l.Code, 7)
	for i := range len(l.Code) {
		assert.Contains(t, shortlink.Alphabet, string(l.Code[i]))
	}
	assert.Equal(t, "https://example.com/page", l.URL)
	assert.False(t, l.CreatedAt.IsZero())
	assert.True(t, l.ExpiresAt.IsZero())
	assert.True(t, l.DeactivatedAt.IsZero())
}

func TestCreate_CodeLengthOption(t *testing.T) {
	t.Parallel()
	mgr := shortlink.New(shortlink.NewMemoryStore(), shortlink.WithCodeLength(12))
	l, err := mgr.Create(context.Background(), shortlink.CreateParams{URL: "https://example.com"})
	require.NoError(t, err)
	assert.Len(t, l.Code, 12)
}

func TestCreate_VanityCode(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mgr := shortlink.New(shortlink.NewMemoryStore())

	l, err := mgr.Create(ctx, shortlink.CreateParams{URL: "https://example.com", Code: "summer-Sale_24"})
	require.NoError(t, err)
	assert.Equal(t, "summer-Sale_24", l.Code)

	_, err = mgr.Create(ctx, shortlink.CreateParams{URL: "https://example.com", Code: "summer-Sale_24"})
	assert.ErrorIs(t, err, shortlink.ErrDuplicate)
}

func TestCreate_VanityValidation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mgr := shortlink.New(shortlink.NewMemoryStore())

	for _, code := range []string{"has space", "with/slash", "dotted.code", "ünïcode", "per%cent"} {
		_, err := mgr.Create(ctx, shortlink.CreateParams{URL: "https://example.com", Code: code})
		assert.ErrorIs(t, err, shortlink.ErrInvalidCode, "code %q", code)
	}

	_, err := mgr.Create(ctx, shortlink.CreateParams{URL: "https://example.com", Code: strings.Repeat("a", 65)})
	assert.ErrorIs(t, err, shortlink.ErrInvalidCode)

	_, err = mgr.Create(ctx, shortlink.CreateParams{URL: "https://example.com", Code: strings.Repeat("a", 64)})
	assert.NoError(t, err)
}

func TestCreate_ReservedCodes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mgr := shortlink.New(shortlink.NewMemoryStore(), shortlink.WithReservedCodes("Promo"))

	for _, code := range []string{"admin", "Admin", "API", "login", "promo", "PROMO"} {
		_, err := mgr.Create(ctx, shortlink.CreateParams{URL: "https://example.com", Code: code})
		assert.ErrorIs(t, err, shortlink.ErrReservedCode, "code %q", code)
	}
}

func TestCreate_URLValidation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mgr := shortlink.New(shortlink.NewMemoryStore())

	// Not absolute, no host, or unparseable — including scheme-only
	// payloads like javascript: and data: that carry no host at all.
	for _, u := range []string{"", "/relative/path", "example.com/no-scheme", "https://", "::bad::", "javascript:alert(1)", "data:text/html,x", "mailto:x@example.com"} {
		_, err := mgr.Create(ctx, shortlink.CreateParams{URL: u})
		assert.ErrorIs(t, err, shortlink.ErrInvalidURL, "url %q", u)
	}

	// Well-formed but outside the default http/https allowlist.
	for _, u := range []string{"ftp://example.com/f", "javascript://example.com/alert"} {
		_, err := mgr.Create(ctx, shortlink.CreateParams{URL: u})
		assert.ErrorIs(t, err, shortlink.ErrSchemeNotAllowed, "url %q", u)
	}

	_, err := mgr.Create(ctx, shortlink.CreateParams{URL: "HTTPS://example.com/ok"})
	assert.NoError(t, err, "scheme match is case-insensitive")
}

func TestCreate_SchemeAllowlist(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mgr := shortlink.New(shortlink.NewMemoryStore(), shortlink.WithSchemes("https"))

	_, err := mgr.Create(ctx, shortlink.CreateParams{URL: "http://example.com"})
	assert.ErrorIs(t, err, shortlink.ErrSchemeNotAllowed)

	_, err = mgr.Create(ctx, shortlink.CreateParams{URL: "https://example.com"})
	assert.NoError(t, err)
}

// collidingStore forces Create collisions to exercise retry behavior.
type collidingStore struct {
	shortlink.Store
	failures int
	calls    int
}

func (s *collidingStore) Create(ctx context.Context, l shortlink.Link) error {
	s.calls++
	if s.calls <= s.failures {
		return shortlink.ErrDuplicate
	}
	return s.Store.Create(ctx, l)
}

func TestCreate_CollisionRetry(t *testing.T) {
	t.Parallel()
	store := &collidingStore{Store: shortlink.NewMemoryStore(), failures: 3}
	mgr := shortlink.New(store)

	l, err := mgr.Create(context.Background(), shortlink.CreateParams{URL: "https://example.com"})
	require.NoError(t, err)
	assert.NotEmpty(t, l.Code)
	assert.Equal(t, 4, store.calls)
}

func TestCreate_CollisionExhausted(t *testing.T) {
	t.Parallel()
	store := &collidingStore{Store: shortlink.NewMemoryStore(), failures: 1000}
	mgr := shortlink.New(store)

	_, err := mgr.Create(context.Background(), shortlink.CreateParams{URL: "https://example.com"})
	assert.ErrorIs(t, err, shortlink.ErrCodeExhausted)
	assert.Equal(t, 5, store.calls)
}

func TestCreate_VanityNoRetry(t *testing.T) {
	t.Parallel()
	store := &collidingStore{Store: shortlink.NewMemoryStore(), failures: 1}
	mgr := shortlink.New(store)

	_, err := mgr.Create(context.Background(), shortlink.CreateParams{URL: "https://example.com", Code: "taken"})
	assert.ErrorIs(t, err, shortlink.ErrDuplicate)
	assert.Equal(t, 1, store.calls, "vanity collisions must not be retried")
}

func TestGet_And_List(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mgr := shortlink.New(shortlink.NewMemoryStore())

	a, err := mgr.Create(ctx, shortlink.CreateParams{URL: "https://example.com/a", Tenant: "t1"})
	require.NoError(t, err)
	_, err = mgr.Create(ctx, shortlink.CreateParams{URL: "https://example.com/b", Tenant: "t2"})
	require.NoError(t, err)

	got, err := mgr.Get(ctx, a.Code)
	require.NoError(t, err)
	assert.Equal(t, a.URL, got.URL)

	_, err = mgr.Get(ctx, "missing")
	assert.ErrorIs(t, err, shortlink.ErrNotFound)

	all, err := mgr.List(ctx, shortlink.Filter{})
	require.NoError(t, err)
	assert.Len(t, all, 2)

	t1, err := mgr.List(ctx, shortlink.Filter{Tenant: "t1"})
	require.NoError(t, err)
	require.Len(t, t1, 1)
	assert.Equal(t, a.Code, t1[0].Code)
}

func TestDeactivate_Activate_Delete(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mgr := shortlink.New(shortlink.NewMemoryStore())

	l, err := mgr.Create(ctx, shortlink.CreateParams{URL: "https://example.com"})
	require.NoError(t, err)

	require.NoError(t, mgr.Deactivate(ctx, l.Code))
	_, err = mgr.Resolve(ctx, l.Code)
	assert.ErrorIs(t, err, shortlink.ErrLinkDeactivated)

	got, err := mgr.Get(ctx, l.Code)
	require.NoError(t, err)
	assert.False(t, got.DeactivatedAt.IsZero())

	require.NoError(t, mgr.Activate(ctx, l.Code))
	_, err = mgr.Resolve(ctx, l.Code)
	assert.NoError(t, err)

	require.NoError(t, mgr.Delete(ctx, l.Code))
	_, err = mgr.Get(ctx, l.Code)
	assert.ErrorIs(t, err, shortlink.ErrNotFound)

	assert.ErrorIs(t, mgr.Deactivate(ctx, l.Code), shortlink.ErrNotFound)
	assert.ErrorIs(t, mgr.Activate(ctx, l.Code), shortlink.ErrNotFound)
	assert.ErrorIs(t, mgr.Delete(ctx, l.Code), shortlink.ErrNotFound)
}

func TestDelete_FreesCode(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mgr := shortlink.New(shortlink.NewMemoryStore())

	_, err := mgr.Create(ctx, shortlink.CreateParams{URL: "https://example.com/old", Code: "reuse"})
	require.NoError(t, err)
	require.NoError(t, mgr.Delete(ctx, "reuse"))

	l, err := mgr.Create(ctx, shortlink.CreateParams{URL: "https://example.com/new", Code: "reuse"})
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/new", l.URL)
}

func TestCreate_Expiry(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mgr := shortlink.New(shortlink.NewMemoryStore())

	exp := time.Now().UTC().Add(time.Hour)
	l, err := mgr.Create(ctx, shortlink.CreateParams{URL: "https://example.com", ExpiresAt: exp})
	require.NoError(t, err)
	assert.Equal(t, exp, l.ExpiresAt)
}
