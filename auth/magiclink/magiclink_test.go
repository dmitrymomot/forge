package magiclink_test

import (
	"context"
	"encoding/base64"
	"errors"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/magiclink"
	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/crypto/keyset"
	"github.com/dmitrymomot/forge/crypto/secret"
	"github.com/dmitrymomot/forge/resilience/cache"
)

type loginClaims struct {
	UserID string `json:"uid"`
}

var testKey = []byte("0123456789abcdef0123456789abcdef")

func TestNewValidation(t *testing.T) {
	_, err := magiclink.New[loginClaims](testKey, "")
	require.Error(t, err, "empty purpose must be rejected")

	_, err = magiclink.New[loginClaims](testKey, "login", magiclink.WithTTL(0))
	require.Error(t, err, "zero TTL must be rejected")

	_, err = magiclink.New[loginClaims](testKey, "login", magiclink.WithTTL(-time.Minute))
	require.Error(t, err, "negative TTL must be rejected")

	_, err = magiclink.New[loginClaims](testKey, "login", magiclink.WithClock(nil))
	require.Error(t, err, "nil clock must be rejected")

	_, err = magiclink.New[loginClaims](testKey, "login", magiclink.WithEncrypt(nil))
	require.Error(t, err, "nil box must be rejected")

	_, err = magiclink.New[loginClaims](nil, "login")
	require.Error(t, err, "empty key must be rejected")
}

func TestStatelessRoundTrip(t *testing.T) {
	m, err := magiclink.New[loginClaims](testKey, "login")
	require.NoError(t, err)

	link, err := m.Issue(context.Background(), loginClaims{UserID: "u_1"})
	require.NoError(t, err)

	got, err := m.Peek(context.Background(), link)
	require.NoError(t, err)
	assert.Equal(t, "u_1", got.UserID)

	// Stateless Redeem is verify-only and multi-use by design.
	got, err = m.Redeem(context.Background(), link)
	require.NoError(t, err)
	assert.Equal(t, "u_1", got.UserID)

	got, err = m.Redeem(context.Background(), link)
	require.NoError(t, err)
	assert.Equal(t, "u_1", got.UserID)
}

func TestExpired(t *testing.T) {
	clk := clock.NewMock(time.Now())
	m, err := magiclink.New[loginClaims](testKey, "login",
		magiclink.WithTTL(15*time.Minute), magiclink.WithClock(clk))
	require.NoError(t, err)

	link, err := m.Issue(context.Background(), loginClaims{UserID: "u_1"})
	require.NoError(t, err)

	clk.Advance(16 * time.Minute)

	_, err = m.Peek(context.Background(), link)
	assert.ErrorIs(t, err, magiclink.ErrExpired)
	_, err = m.Redeem(context.Background(), link)
	assert.ErrorIs(t, err, magiclink.ErrExpired)
}

func TestInvalidTokens(t *testing.T) {
	m, err := magiclink.New[loginClaims](testKey, "login")
	require.NoError(t, err)

	link, err := m.Issue(context.Background(), loginClaims{UserID: "u_1"})
	require.NoError(t, err)

	// Tampered body: flip a character in the first segment.
	tampered := "A" + link[1:]
	if tampered == link {
		tampered = "B" + link[1:]
	}
	for _, bad := range []string{"", "garbage", "a.b", tampered} {
		_, err = m.Redeem(context.Background(), bad)
		assert.ErrorIs(t, err, magiclink.ErrInvalid, "input %q", bad)
	}
}

func TestCrossPurposeRejected(t *testing.T) {
	login, err := magiclink.New[loginClaims](testKey, "login")
	require.NoError(t, err)
	unsub, err := magiclink.New[loginClaims](testKey, "unsubscribe")
	require.NoError(t, err)

	link, err := login.Issue(context.Background(), loginClaims{UserID: "u_1"})
	require.NoError(t, err)

	_, err = unsub.Redeem(context.Background(), link)
	assert.ErrorIs(t, err, magiclink.ErrInvalid)
}

func TestEncryptedPayloadHidden(t *testing.T) {
	box, err := secret.New(testKey)
	require.NoError(t, err)
	m, err := magiclink.New[loginClaims](testKey, "login", magiclink.WithEncrypt(box))
	require.NoError(t, err)

	link, err := m.Issue(context.Background(), loginClaims{UserID: "u_1"})
	require.NoError(t, err)

	// Without encryption the base64 body decodes to plaintext JSON
	// containing the payload; with WithEncrypt it must not.
	body, _, ok := strings.Cut(link, ".")
	require.True(t, ok)
	raw, err := base64.RawURLEncoding.DecodeString(body)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "u_1")

	got, err := m.Redeem(context.Background(), link)
	require.NoError(t, err)
	assert.Equal(t, "u_1", got.UserID)
}

func TestFromKeysetRotation(t *testing.T) {
	old, err := keyset.New(keyset.WithPrimary(1, testKey))
	require.NoError(t, err)
	mOld, err := magiclink.FromKeyset[loginClaims](old, "login")
	require.NoError(t, err)

	link, err := mOld.Issue(context.Background(), loginClaims{UserID: "u_1"})
	require.NoError(t, err)

	rotated, err := keyset.New(
		keyset.WithPrimary(2, []byte("fedcba9876543210fedcba9876543210")),
		keyset.WithRetired(1, testKey),
	)
	require.NoError(t, err)
	mNew, err := magiclink.FromKeyset[loginClaims](rotated, "login")
	require.NoError(t, err)

	// Link signed under the retired key still verifies after rotation.
	got, err := mNew.Redeem(context.Background(), link)
	require.NoError(t, err)
	assert.Equal(t, "u_1", got.UserID)
}

func newMemStore(t *testing.T) cache.Store {
	t.Helper()
	s := cache.NewMemoryStore()
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestSingleUseRedeem(t *testing.T) {
	m, err := magiclink.New[loginClaims](testKey, "login", magiclink.WithStore(newMemStore(t)))
	require.NoError(t, err)

	link, err := m.Issue(context.Background(), loginClaims{UserID: "u_1"})
	require.NoError(t, err)

	got, err := m.Redeem(context.Background(), link)
	require.NoError(t, err)
	assert.Equal(t, "u_1", got.UserID)

	_, err = m.Redeem(context.Background(), link)
	assert.ErrorIs(t, err, magiclink.ErrUsed)
}

func TestPeekDoesNotConsume(t *testing.T) {
	m, err := magiclink.New[loginClaims](testKey, "login", magiclink.WithStore(newMemStore(t)))
	require.NoError(t, err)

	link, err := m.Issue(context.Background(), loginClaims{UserID: "u_1"})
	require.NoError(t, err)

	_, err = m.Peek(context.Background(), link)
	require.NoError(t, err)
	_, err = m.Peek(context.Background(), link)
	require.NoError(t, err, "Peek must be repeatable")

	_, err = m.Redeem(context.Background(), link)
	require.NoError(t, err, "Redeem must still succeed after Peek")

	_, err = m.Peek(context.Background(), link)
	assert.ErrorIs(t, err, magiclink.ErrUsed, "Peek after Redeem reports used")
}

func TestConcurrentRedeemSingleWinner(t *testing.T) {
	m, err := magiclink.New[loginClaims](testKey, "login", magiclink.WithStore(newMemStore(t)))
	require.NoError(t, err)

	link, err := m.Issue(context.Background(), loginClaims{UserID: "u_1"})
	require.NoError(t, err)

	const n = 32
	var wins, used atomic.Int32
	var wg sync.WaitGroup
	for range n {
		wg.Go(func() {
			_, err := m.Redeem(context.Background(), link)
			switch {
			case err == nil:
				wins.Add(1)
			case errors.Is(err, magiclink.ErrUsed):
				used.Add(1)
			}
		})
	}
	wg.Wait()
	assert.Equal(t, int32(1), wins.Load(), "exactly one redeem wins")
	assert.Equal(t, int32(n-1), used.Load(), "all others see ErrUsed")
}

type failingStore struct{ err error }

func (f failingStore) Get(context.Context, string) ([]byte, error) { return nil, f.err }
func (f failingStore) Set(context.Context, string, []byte, ...cache.SetOption) error {
	return f.err
}
func (f failingStore) Delete(context.Context, string) error      { return f.err }
func (f failingStore) Has(context.Context, string) (bool, error) { return false, f.err }
func (f failingStore) DeletePrefix(context.Context, string) error {
	return f.err
}
func (f failingStore) Close() error { return nil }

func TestStoreFailureFailsClosed(t *testing.T) {
	m, err := magiclink.New[loginClaims](testKey, "login",
		magiclink.WithStore(failingStore{err: errors.New("boom")}))
	require.NoError(t, err)

	link, err := m.Issue(context.Background(), loginClaims{UserID: "u_1"})
	require.NoError(t, err)

	_, err = m.Redeem(context.Background(), link)
	assert.ErrorIs(t, err, magiclink.ErrStore)
	_, err = m.Peek(context.Background(), link)
	assert.ErrorIs(t, err, magiclink.ErrStore)
}

func TestJunkRejectedBeforeStore(t *testing.T) {
	// Signature check precedes store I/O: junk must yield ErrInvalid even
	// when every store call would fail.
	m, err := magiclink.New[loginClaims](testKey, "login",
		magiclink.WithStore(failingStore{err: errors.New("boom")}))
	require.NoError(t, err)

	_, err = m.Redeem(context.Background(), "garbage.token")
	assert.ErrorIs(t, err, magiclink.ErrInvalid)
}

func TestWithStoreNilRejected(t *testing.T) {
	_, err := magiclink.New[loginClaims](testKey, "login", magiclink.WithStore(nil))
	require.Error(t, err)
}

type tenantKey struct{}

func tenantCtx(scope string) context.Context {
	return context.WithValue(context.Background(), tenantKey{}, scope)
}

func tenantScope(ctx context.Context) (string, error) {
	v, _ := ctx.Value(tenantKey{}).(string)
	return v, nil
}

func TestScopeMatrix(t *testing.T) {
	m, err := magiclink.New[loginClaims](testKey, "invite", magiclink.WithScope(tenantScope))
	require.NoError(t, err)

	// Global link (issued without tenant in ctx): valid everywhere.
	global, err := m.Issue(context.Background(), loginClaims{UserID: "u_1"})
	require.NoError(t, err)
	_, err = m.Redeem(tenantCtx("acme"), global)
	require.NoError(t, err, "global link redeems inside a tenant")
	_, err = m.Redeem(context.Background(), global)
	require.NoError(t, err, "global link redeems globally")

	// Scoped link: valid only in the exact tenant it was issued in.
	scoped, err := m.Issue(tenantCtx("acme"), loginClaims{UserID: "u_1"})
	require.NoError(t, err)
	_, err = m.Peek(tenantCtx("acme"), scoped)
	require.NoError(t, err, "Peek applies the same scope rule")
	_, err = m.Redeem(tenantCtx("acme"), scoped)
	require.NoError(t, err)
	_, err = m.Redeem(tenantCtx("globex"), scoped)
	assert.ErrorIs(t, err, magiclink.ErrScopeMismatch)
	_, err = m.Redeem(context.Background(), scoped)
	assert.ErrorIs(t, err, magiclink.ErrScopeMismatch, "scoped link is not global")
}

func TestScopeHookErrorPropagates(t *testing.T) {
	hookErr := errors.New("no tenant in ctx")
	m, err := magiclink.New[loginClaims](testKey, "invite",
		magiclink.WithScope(func(ctx context.Context) (string, error) {
			if v, ok := ctx.Value(tenantKey{}).(string); ok {
				return v, nil
			}
			return "", hookErr
		}))
	require.NoError(t, err)

	_, err = m.Issue(context.Background(), loginClaims{UserID: "u_1"})
	assert.ErrorIs(t, err, hookErr, "hook error aborts issuance")

	link, err := m.Issue(tenantCtx("acme"), loginClaims{UserID: "u_1"})
	require.NoError(t, err)
	_, err = m.Redeem(context.Background(), link)
	assert.ErrorIs(t, err, hookErr, "hook error aborts redemption")
}

func TestScopedTokenOnUnscopedManagerRejected(t *testing.T) {
	scoped, err := magiclink.New[loginClaims](testKey, "invite", magiclink.WithScope(tenantScope))
	require.NoError(t, err)
	plain, err := magiclink.New[loginClaims](testKey, "invite")
	require.NoError(t, err)

	link, err := scoped.Issue(tenantCtx("acme"), loginClaims{UserID: "u_1"})
	require.NoError(t, err)

	// Config drift fails closed: no hook means ctx scope is always "".
	_, err = plain.Redeem(context.Background(), link)
	assert.ErrorIs(t, err, magiclink.ErrScopeMismatch)
}

func TestWithScopeNilRejected(t *testing.T) {
	_, err := magiclink.New[loginClaims](testKey, "invite", magiclink.WithScope(nil))
	require.Error(t, err)
}

func TestIssueURLDefaultBase(t *testing.T) {
	m, err := magiclink.New[loginClaims](testKey, "login",
		magiclink.WithBaseURL("https://app.example.com/auth/verify"))
	require.NoError(t, err)

	u, err := m.IssueURL(context.Background(), "", loginClaims{UserID: "u_1"})
	require.NoError(t, err)

	parsed, err := url.Parse(u)
	require.NoError(t, err)
	assert.Equal(t, "https", parsed.Scheme)
	assert.Equal(t, "app.example.com", parsed.Host)
	assert.Equal(t, "/auth/verify", parsed.Path)

	link := parsed.Query().Get("token")
	require.NotEmpty(t, link)
	got, err := m.Redeem(context.Background(), link)
	require.NoError(t, err)
	assert.Equal(t, "u_1", got.UserID)
}

func TestIssueURLPerCallBaseWins(t *testing.T) {
	m, err := magiclink.New[loginClaims](testKey, "invite",
		magiclink.WithBaseURL("https://app.example.com/join"))
	require.NoError(t, err)

	u, err := m.IssueURL(context.Background(), "https://acme.example.com/join",
		loginClaims{UserID: "u_1"})
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(u, "https://acme.example.com/join?token="), u)
}

func TestIssueURLNoBaseErrors(t *testing.T) {
	m, err := magiclink.New[loginClaims](testKey, "login")
	require.NoError(t, err)

	_, err = m.IssueURL(context.Background(), "", loginClaims{UserID: "u_1"})
	require.Error(t, err)
}

func TestIssueURLPreservesExistingQuery(t *testing.T) {
	m, err := magiclink.New[loginClaims](testKey, "login")
	require.NoError(t, err)

	u, err := m.IssueURL(context.Background(),
		"https://app.example.com/verify?lang=de", loginClaims{UserID: "u_1"})
	require.NoError(t, err)

	parsed, err := url.Parse(u)
	require.NoError(t, err)
	assert.Equal(t, "de", parsed.Query().Get("lang"))
	assert.NotEmpty(t, parsed.Query().Get("token"))
}

func TestWithParamRename(t *testing.T) {
	m, err := magiclink.New[loginClaims](testKey, "login", magiclink.WithParam("t"))
	require.NoError(t, err)

	u, err := m.IssueURL(context.Background(), "https://app.example.com/verify",
		loginClaims{UserID: "u_1"})
	require.NoError(t, err)

	parsed, err := url.Parse(u)
	require.NoError(t, err)
	assert.NotEmpty(t, parsed.Query().Get("t"))
	assert.Empty(t, parsed.Query().Get("token"))
}

func TestURLOptionValidation(t *testing.T) {
	_, err := magiclink.New[loginClaims](testKey, "login", magiclink.WithParam(""))
	require.Error(t, err, "empty param name must be rejected")

	_, err = magiclink.New[loginClaims](testKey, "login", magiclink.WithBaseURL(""))
	require.Error(t, err, "empty base URL must be rejected")

	_, err = magiclink.New[loginClaims](testKey, "login", magiclink.WithBaseURL("://bad"))
	require.Error(t, err, "unparsable base URL must be rejected")
}

func TestIssueURLBadBaseErrors(t *testing.T) {
	m, err := magiclink.New[loginClaims](testKey, "login")
	require.NoError(t, err)

	// An unparsable per-call base is used directly (no fallback) and must
	// surface the url.Parse failure.
	_, err = m.IssueURL(context.Background(), "://bad", loginClaims{UserID: "u_1"})
	require.Error(t, err)
}

func TestIssueURLScopeHookError(t *testing.T) {
	hookErr := errors.New("no tenant in ctx")
	m, err := magiclink.New[loginClaims](testKey, "login",
		magiclink.WithScope(func(ctx context.Context) (string, error) {
			if v, ok := ctx.Value(tenantKey{}).(string); ok {
				return v, nil
			}
			return "", hookErr
		}))
	require.NoError(t, err)

	// IssueURL delegates issuance to Issue, so the scope-hook error must
	// propagate through IssueURL -> Issue -> resolveScope.
	_, err = m.IssueURL(context.Background(), "https://app.example.com/verify",
		loginClaims{UserID: "u_1"})
	assert.ErrorIs(t, err, hookErr)
}
