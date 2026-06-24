package cookie_test

import (
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/pkg/cookie"
)

const testSecret = "this-is-a-32-byte-or-longer-key!"

func mustNew(t *testing.T, cfg cookie.Config) *cookie.Manager {
	t.Helper()
	m, err := cookie.New(cfg)
	require.NoError(t, err)
	require.NotNil(t, m)
	return m
}

func TestNew(t *testing.T) {
	t.Parallel()

	m, err := cookie.New(cookie.Config{})
	require.NoError(t, err)
	require.NotNil(t, m)
}

func TestNewWithConfig(t *testing.T) {
	t.Parallel()

	m, err := cookie.New(cookie.Config{
		Secret:   testSecret,
		Domain:   "example.com",
		Path:     "/app",
		Secure:   true,
		HTTPOnly: true,
		SameSite: "strict",
	})
	require.NoError(t, err)
	require.NotNil(t, m)
}

func TestNewBadSecret(t *testing.T) {
	t.Parallel()

	_, err := cookie.New(cookie.Config{Secret: "short"})
	require.ErrorIs(t, err, cookie.ErrBadSecret)
}

func TestPlainCookies(t *testing.T) {
	t.Parallel()

	m := mustNew(t, cookie.Config{})

	t.Run("get non-existent cookie", func(t *testing.T) {
		t.Parallel()

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		_, err := m.Get(r, "missing")
		require.ErrorIs(t, err, cookie.ErrNotFound)
	})

	t.Run("set and get cookie", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		m.Set(w, "name", "value", 3600)

		cookies := w.Result().Cookies()
		require.Len(t, cookies, 1)

		c := cookies[0]
		require.Equal(t, "name", c.Name)
		require.Equal(t, "value", c.Value)
		require.Equal(t, 3600, c.MaxAge)

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.AddCookie(c)

		val, err := m.Get(r, "name")
		require.NoError(t, err)
		require.Equal(t, "value", val)
	})

	t.Run("delete cookie", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		m.Delete(w, "name")

		cookies := w.Result().Cookies()
		require.Len(t, cookies, 1)
		require.Equal(t, -1, cookies[0].MaxAge)
	})
}

func TestSignedCookies(t *testing.T) {
	t.Parallel()

	t.Run("no secret returns error", func(t *testing.T) {
		t.Parallel()

		m := mustNew(t, cookie.Config{}) // no secret
		w := httptest.NewRecorder()

		err := m.SetSigned(w, "session", "data", 3600)
		require.ErrorIs(t, err, cookie.ErrNoSecret)

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		_, err = m.GetSigned(r, "session")
		require.ErrorIs(t, err, cookie.ErrNoSecret)
	})

	t.Run("short secret returns error on New", func(t *testing.T) {
		t.Parallel()

		_, err := cookie.New(cookie.Config{Secret: "short"})
		require.ErrorIs(t, err, cookie.ErrBadSecret)
	})

	t.Run("set and get signed cookie", func(t *testing.T) {
		t.Parallel()

		m := mustNew(t, cookie.Config{Secret: testSecret})

		w := httptest.NewRecorder()
		require.NoError(t, m.SetSigned(w, "session", "user123", 3600))

		cookies := w.Result().Cookies()
		require.Len(t, cookies, 1)

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.AddCookie(cookies[0])

		val, err := m.GetSigned(r, "session")
		require.NoError(t, err)
		require.Equal(t, "user123", val)
	})

	t.Run("tampered cookie fails", func(t *testing.T) {
		t.Parallel()

		m := mustNew(t, cookie.Config{Secret: testSecret})

		w := httptest.NewRecorder()
		require.NoError(t, m.SetSigned(w, "session", "user123", 3600))

		c := w.Result().Cookies()[0]
		c.Value = "dGFtcGVyZWQ.invalid"

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.AddCookie(c)

		_, err := m.GetSigned(r, "session")
		require.ErrorIs(t, err, cookie.ErrBadSig)
	})

	t.Run("missing cookie returns not found", func(t *testing.T) {
		t.Parallel()

		m := mustNew(t, cookie.Config{Secret: testSecret})
		r := httptest.NewRequest(http.MethodGet, "/", nil)

		_, err := m.GetSigned(r, "missing")
		require.ErrorIs(t, err, cookie.ErrNotFound)
	})

	t.Run("value not valid under different name", func(t *testing.T) {
		t.Parallel()

		m := mustNew(t, cookie.Config{Secret: testSecret})

		w := httptest.NewRecorder()
		require.NoError(t, m.SetSigned(w, "session", "user123", 3600))

		// Move the value to a cookie with a different name.
		c := w.Result().Cookies()[0]
		c.Name = "other"

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.AddCookie(c)

		_, err := m.GetSigned(r, "other")
		require.ErrorIs(t, err, cookie.ErrBadSig)
	})

	t.Run("expired signed cookie rejected", func(t *testing.T) {
		t.Parallel()

		m := mustNew(t, cookie.Config{Secret: testSecret})

		w := httptest.NewRecorder()
		require.NoError(t, m.SetSigned(w, "session", "user123", 1))

		c := w.Result().Cookies()[0]
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.AddCookie(c)

		// Wait until the 1s embedded expiry has passed; the value must be rejected.
		time.Sleep(1100 * time.Millisecond)
		_, err := m.GetSigned(r, "session")
		require.ErrorIs(t, err, cookie.ErrExpired)
	})

	t.Run("zero maxAge embeds no expiry", func(t *testing.T) {
		t.Parallel()

		m := mustNew(t, cookie.Config{Secret: testSecret})

		w := httptest.NewRecorder()
		require.NoError(t, m.SetSigned(w, "session", "user123", 0))

		c := w.Result().Cookies()[0]
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.AddCookie(c)

		val, err := m.GetSigned(r, "session")
		require.NoError(t, err)
		require.Equal(t, "user123", val)
	})
}

func TestEncryptedCookies(t *testing.T) {
	t.Parallel()

	t.Run("no secret returns error", func(t *testing.T) {
		t.Parallel()

		m := mustNew(t, cookie.Config{}) // no secret
		w := httptest.NewRecorder()

		err := m.SetEncrypted(w, "data", "secret", 3600)
		require.ErrorIs(t, err, cookie.ErrNoSecret)

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		_, err = m.GetEncrypted(r, "data")
		require.ErrorIs(t, err, cookie.ErrNoSecret)
	})

	t.Run("set and get encrypted cookie", func(t *testing.T) {
		t.Parallel()

		m := mustNew(t, cookie.Config{Secret: testSecret})

		w := httptest.NewRecorder()
		require.NoError(t, m.SetEncrypted(w, "secret", "confidential", 3600))

		cookies := w.Result().Cookies()
		require.Len(t, cookies, 1)
		require.NotEqual(t, "confidential", cookies[0].Value, "cookie value should be encrypted")

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.AddCookie(cookies[0])

		val, err := m.GetEncrypted(r, "secret")
		require.NoError(t, err)
		require.Equal(t, "confidential", val)
	})

	t.Run("tampered cookie fails", func(t *testing.T) {
		t.Parallel()

		m := mustNew(t, cookie.Config{Secret: testSecret})

		w := httptest.NewRecorder()
		require.NoError(t, m.SetEncrypted(w, "secret", "confidential", 3600))

		c := w.Result().Cookies()[0]
		c.Value = "tamperedvalue"

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.AddCookie(c)

		_, err := m.GetEncrypted(r, "secret")
		require.ErrorIs(t, err, cookie.ErrDecrypt)
	})

	t.Run("missing cookie returns not found", func(t *testing.T) {
		t.Parallel()

		m := mustNew(t, cookie.Config{Secret: testSecret})
		r := httptest.NewRequest(http.MethodGet, "/", nil)

		_, err := m.GetEncrypted(r, "missing")
		require.ErrorIs(t, err, cookie.ErrNotFound)
	})

	t.Run("value not valid under different name", func(t *testing.T) {
		t.Parallel()

		m := mustNew(t, cookie.Config{Secret: testSecret})

		w := httptest.NewRecorder()
		require.NoError(t, m.SetEncrypted(w, "secret", "confidential", 3600))

		// Move the ciphertext to a cookie with a different name. The name is the
		// GCM AAD, so authentication must fail.
		c := w.Result().Cookies()[0]
		c.Name = "other"

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.AddCookie(c)

		_, err := m.GetEncrypted(r, "other")
		require.ErrorIs(t, err, cookie.ErrDecrypt)
	})

	t.Run("large binary value round-trips", func(t *testing.T) {
		t.Parallel()

		m := mustNew(t, cookie.Config{Secret: testSecret})

		// 4 KiB of random bytes including NUL and high bytes.
		buf := make([]byte, 4096)
		_, err := rand.Read(buf)
		require.NoError(t, err)
		value := string(buf)

		w := httptest.NewRecorder()
		require.NoError(t, m.SetEncrypted(w, "blob", value, 3600))

		c := w.Result().Cookies()[0]
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.AddCookie(c)

		got, err := m.GetEncrypted(r, "blob")
		require.NoError(t, err)
		require.Equal(t, value, got)
	})

	t.Run("empty value round-trips", func(t *testing.T) {
		t.Parallel()

		m := mustNew(t, cookie.Config{Secret: testSecret})

		w := httptest.NewRecorder()
		require.NoError(t, m.SetEncrypted(w, "empty", "", 3600))

		c := w.Result().Cookies()[0]
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.AddCookie(c)

		got, err := m.GetEncrypted(r, "empty")
		require.NoError(t, err)
		require.Equal(t, "", got)
	})

	t.Run("expired encrypted cookie rejected", func(t *testing.T) {
		t.Parallel()

		m := mustNew(t, cookie.Config{Secret: testSecret})

		w := httptest.NewRecorder()
		require.NoError(t, m.SetEncrypted(w, "secret", "confidential", 1))

		c := w.Result().Cookies()[0]
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.AddCookie(c)

		// Wait until the 1s embedded expiry has passed; GetEncrypted reports it.
		time.Sleep(1100 * time.Millisecond)
		_, err := m.GetEncrypted(r, "secret")
		require.ErrorIs(t, err, cookie.ErrExpired)
	})
}

func TestFlash(t *testing.T) {
	t.Parallel()

	t.Run("no secret returns error", func(t *testing.T) {
		t.Parallel()

		m := mustNew(t, cookie.Config{})
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/", nil)

		err := m.SetFlash(w, "msg", "hello")
		require.ErrorIs(t, err, cookie.ErrNoSecret)

		var dest string
		err = m.Flash(w, r, "msg", &dest)
		require.ErrorIs(t, err, cookie.ErrNoSecret)
	})

	t.Run("set and get flash", func(t *testing.T) {
		t.Parallel()

		m := mustNew(t, cookie.Config{Secret: testSecret})

		w := httptest.NewRecorder()
		msg := map[string]string{"type": "success", "text": "Saved!"}
		require.NoError(t, m.SetFlash(w, "msg", msg))

		cookies := w.Result().Cookies()
		require.Len(t, cookies, 1)
		require.Equal(t, "flash_msg", cookies[0].Name)

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.AddCookie(cookies[0])

		w2 := httptest.NewRecorder()
		var dest map[string]string
		require.NoError(t, m.Flash(w2, r, "msg", &dest))
		require.Equal(t, "success", dest["type"])
		require.Equal(t, "Saved!", dest["text"])

		// Flash emits a delete cookie (MaxAge=-1) after a successful read.
		deleteCookies := w2.Result().Cookies()
		require.Len(t, deleteCookies, 1)
		require.Equal(t, -1, deleteCookies[0].MaxAge)
	})

	t.Run("second read returns not found", func(t *testing.T) {
		t.Parallel()

		m := mustNew(t, cookie.Config{Secret: testSecret})

		w := httptest.NewRecorder()
		require.NoError(t, m.SetFlash(w, "msg", "hello"))
		setCookie := w.Result().Cookies()[0]

		// First read succeeds and consumes the flash.
		r1 := httptest.NewRequest(http.MethodGet, "/", nil)
		r1.AddCookie(setCookie)
		w1 := httptest.NewRecorder()
		var dest string
		require.NoError(t, m.Flash(w1, r1, "msg", &dest))
		require.Equal(t, "hello", dest)

		// Second request lacks the cookie (it was deleted), so re-read is ErrNotFound.
		r2 := httptest.NewRequest(http.MethodGet, "/", nil)
		w2 := httptest.NewRecorder()
		var dest2 string
		err := m.Flash(w2, r2, "msg", &dest2)
		require.ErrorIs(t, err, cookie.ErrNotFound)
	})

	t.Run("missing flash returns not found without delete cookie", func(t *testing.T) {
		t.Parallel()

		m := mustNew(t, cookie.Config{Secret: testSecret})
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/", nil)

		var dest string
		err := m.Flash(w, r, "missing", &dest)
		require.ErrorIs(t, err, cookie.ErrNotFound)

		// Nothing existed to clear, so no delete cookie should be emitted.
		require.Empty(t, w.Result().Cookies())
	})

	t.Run("corrupt flash is deleted on decrypt failure", func(t *testing.T) {
		t.Parallel()

		m := mustNew(t, cookie.Config{Secret: testSecret})

		// Present a flash cookie whose value cannot be decrypted.
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.AddCookie(&http.Cookie{Name: "flash_msg", Value: "not-a-valid-ciphertext"})

		w := httptest.NewRecorder()
		var dest string
		err := m.Flash(w, r, "msg", &dest)
		require.ErrorIs(t, err, cookie.ErrDecrypt)

		// The corrupt cookie must be deleted so it cannot persist.
		deleteCookies := w.Result().Cookies()
		require.Len(t, deleteCookies, 1)
		require.Equal(t, "flash_msg", deleteCookies[0].Name)
		require.Equal(t, -1, deleteCookies[0].MaxAge)
	})
}

func TestSameSiteParsing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		value    string
		want     http.SameSite
		forceSec bool // SameSite=None must force Secure on
	}{
		{name: "strict", value: "strict", want: http.SameSiteStrictMode},
		{name: "lax", value: "lax", want: http.SameSiteLaxMode},
		{name: "none forces secure", value: "none", want: http.SameSiteNoneMode, forceSec: true},
		{name: "uppercase strict", value: "STRICT", want: http.SameSiteStrictMode},
		{name: "invalid falls back to lax", value: "bogus", want: http.SameSiteLaxMode},
		{name: "empty falls back to lax", value: "", want: http.SameSiteLaxMode},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			m := mustNew(t, cookie.Config{SameSite: tc.value})

			w := httptest.NewRecorder()
			m.Set(w, "k", "v", 3600)
			c := w.Result().Cookies()[0]

			require.Equal(t, tc.want, c.SameSite)
			if tc.forceSec {
				require.True(t, c.Secure, "SameSite=None must force Secure on")
			}
		})
	}
}

func TestCookieAttributes(t *testing.T) {
	t.Parallel()

	m := mustNew(t, cookie.Config{
		Secret:   testSecret,
		Domain:   "example.com",
		Path:     "/app",
		Secure:   true,
		HTTPOnly: true,
		SameSite: "strict",
	})

	w := httptest.NewRecorder()
	m.Set(w, "test", "value", 3600)

	c := w.Result().Cookies()[0]
	require.Equal(t, "example.com", c.Domain)
	require.Equal(t, "/app", c.Path)
	require.True(t, c.Secure)
	require.True(t, c.HttpOnly)
	require.Equal(t, http.SameSiteStrictMode, c.SameSite)
}

func TestDefaultAttributes(t *testing.T) {
	t.Parallel()

	m := mustNew(t, cookie.Config{})

	w := httptest.NewRecorder()
	m.Set(w, "test", "value", 3600)

	c := w.Result().Cookies()[0]
	require.Equal(t, "/", c.Path)
	// HttpOnly is a secure default: the Manager always emits HttpOnly cookies.
	require.True(t, c.HttpOnly, "HttpOnly should default to true")
	require.Equal(t, http.SameSiteLaxMode, c.SameSite)
}
