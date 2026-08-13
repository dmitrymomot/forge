package flash_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/crypto/keyset"
	"github.com/dmitrymomot/forge/resilience/cache"
	"github.com/dmitrymomot/forge/view/flash"
	"github.com/dmitrymomot/forge/web/cookie"
)

func newCodec(t *testing.T) *cookie.Codec {
	t.Helper()
	ks, err := keyset.New(keyset.WithPrimary(1, make([]byte, 32)))
	require.NoError(t, err)
	c, err := cookie.New(ks)
	require.NoError(t, err)
	return c
}

func newCookieStore(t *testing.T, opts ...flash.Option) flash.Store {
	t.Helper()
	s, err := flash.NewCookieStore(newCodec(t), opts...)
	require.NoError(t, err)
	return s
}

func newCacheStore(t *testing.T, opts ...flash.Option) flash.Store {
	t.Helper()
	store := cache.NewMemoryStore()
	t.Cleanup(func() { _ = store.Close() })
	s, err := flash.NewCacheStore(store, newCodec(t), opts...)
	require.NoError(t, err)
	return s
}

// carryCookies copies a response's cookies onto req the way a browser would: a jar
// keeps one entry per name, so a second Set-Cookie for the same name replaces the
// first rather than being sent alongside it.
func carryCookies(req *http.Request, res *http.Response) *http.Request {
	latest := map[string]*http.Cookie{}
	order := []string{}
	for _, c := range res.Cookies() {
		if _, seen := latest[c.Name]; !seen {
			order = append(order, c.Name)
		}
		latest[c.Name] = c
	}
	for _, name := range order {
		req.AddCookie(latest[name])
	}
	return req
}

// roundTrip stages msgs on one response, carries the cookies it wrote onto a fresh
// request, and returns what the next request reads — the redirect the package exists
// for, minus the browser.
func roundTrip(t *testing.T, s flash.Store, msgs ...flash.Message) []flash.Message {
	t.Helper()
	setRec := httptest.NewRecorder()
	setReq := httptest.NewRequest(http.MethodPost, "/pay", nil)
	require.NoError(t, s.Set(setRec, setReq, msgs...))

	takeReq := carryCookies(httptest.NewRequest(http.MethodGet, "/invoices", nil), setRec.Result())
	got, err := s.Take(httptest.NewRecorder(), takeReq)
	require.NoError(t, err)
	return got
}

// stores runs a subtest per shipped Store, so both go through the same contract.
func stores(t *testing.T) map[string]func(t *testing.T, opts ...flash.Option) flash.Store {
	t.Helper()
	return map[string]func(t *testing.T, opts ...flash.Option) flash.Store{
		"cookie": newCookieStore,
		"cache":  newCacheStore,
	}
}

func TestStore_RoundTripsOneMessage(t *testing.T) {
	for name, build := range stores(t) {
		t.Run(name, func(t *testing.T) {
			got := roundTrip(t, build(t), flash.Success("the invoice is sent"))
			assert.Equal(t, []flash.Message{flash.Success("the invoice is sent")}, got)
		})
	}
}

func TestStore_RoundTripsEveryLevelAndOrder(t *testing.T) {
	in := []flash.Message{
		flash.Info("one"),
		flash.Success("two"),
		flash.Warning("three"),
		flash.Error("four"),
	}
	for name, build := range stores(t) {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, in, roundTrip(t, build(t), in...))
		})
	}
}

func TestStore_TakeIsOneShot(t *testing.T) {
	for name, build := range stores(t) {
		t.Run(name, func(t *testing.T) {
			s := build(t)
			setRec := httptest.NewRecorder()
			require.NoError(t, s.Set(setRec, httptest.NewRequest(http.MethodPost, "/pay", nil), flash.Info("once")))

			first := carryCookies(httptest.NewRequest(http.MethodGet, "/", nil), setRec.Result())
			takeRec := httptest.NewRecorder()
			got, err := s.Take(takeRec, first)
			require.NoError(t, err)
			require.Len(t, got, 1)

			deleted := carryCookies(httptest.NewRequest(http.MethodGet, "/", nil), takeRec.Result())
			again, err := s.Take(httptest.NewRecorder(), deleted)
			require.NoError(t, err)
			assert.Empty(t, again)
		})
	}
}

func TestStore_NoCookieReadsEmpty(t *testing.T) {
	for name, build := range stores(t) {
		t.Run(name, func(t *testing.T) {
			got, err := build(t).Take(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
			require.NoError(t, err)
			assert.Empty(t, got)
		})
	}
}

func TestStore_EmptyTextWritesNothing(t *testing.T) {
	for name, build := range stores(t) {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			require.NoError(t, build(t).Set(rec, httptest.NewRequest(http.MethodPost, "/", nil), flash.Info("")))
			assert.Empty(t, rec.Result().Cookies())
		})
	}
}

func TestStore_NoMessagesWritesNothing(t *testing.T) {
	for name, build := range stores(t) {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			require.NoError(t, build(t).Set(rec, httptest.NewRequest(http.MethodPost, "/", nil)))
			assert.Empty(t, rec.Result().Cookies())
		})
	}
}

func TestStore_SetReplacesWithinOneResponse(t *testing.T) {
	for name, build := range stores(t) {
		t.Run(name, func(t *testing.T) {
			s := build(t)
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/", nil)
			require.NoError(t, s.Set(rec, req, flash.Info("first")))
			require.NoError(t, s.Set(rec, req, flash.Info("second")))

			next := carryCookies(httptest.NewRequest(http.MethodGet, "/", nil), rec.Result())
			got, err := s.Take(httptest.NewRecorder(), next)
			require.NoError(t, err)
			assert.Equal(t, []flash.Message{flash.Info("second")}, got)
		})
	}
}

func TestStore_TamperedCookieReadsEmpty(t *testing.T) {
	for name, build := range stores(t) {
		t.Run(name, func(t *testing.T) {
			s := build(t)
			setRec := httptest.NewRecorder()
			require.NoError(t, s.Set(setRec, httptest.NewRequest(http.MethodPost, "/", nil), flash.Error("boom")))

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			for _, c := range setRec.Result().Cookies() {
				c.Value = "forged" + c.Value
				req.AddCookie(c)
			}
			got, err := s.Take(httptest.NewRecorder(), req)
			require.NoError(t, err)
			assert.Empty(t, got)
		})
	}
}

func TestStore_CustomCookieName(t *testing.T) {
	for name, build := range stores(t) {
		t.Run(name, func(t *testing.T) {
			s := build(t, flash.WithCookieName("toast"))
			rec := httptest.NewRecorder()
			require.NoError(t, s.Set(rec, httptest.NewRequest(http.MethodPost, "/", nil), flash.Info("hi")))

			cookies := rec.Result().Cookies()
			require.Len(t, cookies, 1)
			assert.Equal(t, "toast", cookies[0].Name)
		})
	}
}

func TestStore_LifetimeBoundsTheCookie(t *testing.T) {
	for name, build := range stores(t) {
		t.Run(name, func(t *testing.T) {
			s := build(t, flash.WithLifetime(90*time.Second))
			rec := httptest.NewRecorder()
			require.NoError(t, s.Set(rec, httptest.NewRequest(http.MethodPost, "/", nil), flash.Info("hi")))

			cookies := rec.Result().Cookies()
			require.Len(t, cookies, 1)
			assert.Equal(t, 90, cookies[0].MaxAge)
		})
	}
}

func TestCookieStore_RejectsOversizedPayload(t *testing.T) {
	s := newCookieStore(t)
	err := s.Set(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", nil),
		flash.Info(strings.Repeat("x", flash.MaxCookieBytes+1)))
	require.Error(t, err)
	assert.ErrorIs(t, err, flash.ErrTooLarge)
}

// TestCacheStore_HoldsLongText is the reason CacheStore exists: the same payload the
// cookie store refuses rides a server-side entry with only a ticket on the wire.
func TestCacheStore_HoldsLongText(t *testing.T) {
	long := strings.Repeat("x", flash.MaxCookieBytes+1)
	got := roundTrip(t, newCacheStore(t), flash.Info(long))
	require.Len(t, got, 1)
	assert.Equal(t, long, got[0].Text)
}

func TestCacheStore_TicketCookieCarriesNoPayload(t *testing.T) {
	s := newCacheStore(t)
	rec := httptest.NewRecorder()
	require.NoError(t, s.Set(rec, httptest.NewRequest(http.MethodPost, "/", nil), flash.Error("the secret text")))

	cookies := rec.Result().Cookies()
	require.Len(t, cookies, 1)
	assert.NotContains(t, cookies[0].Value, "secret")
}

func TestCacheStore_EvictedEntryReadsEmpty(t *testing.T) {
	store := cache.NewMemoryStore()
	t.Cleanup(func() { _ = store.Close() })
	s, err := flash.NewCacheStore(store, newCodec(t))
	require.NoError(t, err)

	setRec := httptest.NewRecorder()
	setReq := httptest.NewRequest(http.MethodPost, "/", nil)
	require.NoError(t, s.Set(setRec, setReq, flash.Info("gone")))
	require.NoError(t, store.DeletePrefix(setReq.Context(), flash.KeyPrefix))

	next := carryCookies(httptest.NewRequest(http.MethodGet, "/", nil), setRec.Result())
	got, err := s.Take(httptest.NewRecorder(), next)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestConstructors_RejectNilDependencies(t *testing.T) {
	_, err := flash.NewCookieStore(nil)
	assert.ErrorIs(t, err, flash.ErrInvalidConfig)

	_, err = flash.NewCacheStore(nil, newCodec(t))
	assert.ErrorIs(t, err, flash.ErrInvalidConfig)

	_, err = flash.NewCacheStore(cache.NewMemoryStore(), nil)
	assert.ErrorIs(t, err, flash.ErrInvalidConfig)
}

func TestOptions_RejectInvalidValues(t *testing.T) {
	_, err := flash.NewCookieStore(newCodec(t), flash.WithCookieName(""))
	assert.ErrorIs(t, err, flash.ErrInvalidConfig)

	_, err = flash.NewCookieStore(newCodec(t), flash.WithLifetime(0))
	assert.ErrorIs(t, err, flash.ErrInvalidConfig)

	_, err = flash.NewCookieStore(newCodec(t), flash.WithLifetime(-time.Second))
	assert.ErrorIs(t, err, flash.ErrInvalidConfig)
}

func TestSetter_StagesThroughTheStore(t *testing.T) {
	s := newCookieStore(t)
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	rec := httptest.NewRecorder()

	require.NoError(t, flash.Setter(s, req, flash.Success("done"))(rec))

	next := carryCookies(httptest.NewRequest(http.MethodGet, "/", nil), rec.Result())
	got, err := s.Take(httptest.NewRecorder(), next)
	require.NoError(t, err)
	assert.Equal(t, []flash.Message{flash.Success("done")}, got)
}

func TestLevel_Valid(t *testing.T) {
	for _, l := range []flash.Level{flash.LevelInfo, flash.LevelSuccess, flash.LevelWarning, flash.LevelError} {
		assert.True(t, l.Valid())
	}
	assert.False(t, flash.Level("script").Valid())
	assert.False(t, flash.Level("").Valid())
}

// TestCookieStore_OversizeAfterEncodingStillReportsErrTooLarge covers the band where
// base64 and the signature push a payload under MaxCookieBytes past the codec's
// 4096-byte encoded limit: one errors.Is must catch both caps.
func TestCookieStore_OversizeAfterEncodingStillReportsErrTooLarge(t *testing.T) {
	s := newCookieStore(t)
	err := s.Set(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", nil),
		flash.Info(strings.Repeat("x", 3000)))

	require.Error(t, err)
	assert.ErrorIs(t, err, flash.ErrTooLarge)
}
