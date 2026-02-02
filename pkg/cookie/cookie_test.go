package cookie_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmitrymomot/forge/pkg/cookie"
)

const testSecret = "this-is-a-32-byte-or-longer-key!"

func TestNew(t *testing.T) {
	m := cookie.New()
	if m == nil {
		t.Fatal("New() returned nil")
	}
}

func TestNewWithOptions(t *testing.T) {
	m := cookie.New(
		cookie.WithSecret(testSecret),
		cookie.WithDomain("example.com"),
		cookie.WithPath("/app"),
		cookie.WithSecure(true),
		cookie.WithHTTPOnly(true),
		cookie.WithSameSite(http.SameSiteStrictMode),
	)
	if m == nil {
		t.Fatal("New() returned nil")
	}
}

func TestPlainCookies(t *testing.T) {
	m := cookie.New()

	t.Run("get non-existent cookie", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		_, err := m.Get(r, "missing")
		if !errors.Is(err, cookie.ErrNotFound) {
			t.Errorf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("set and get cookie", func(t *testing.T) {
		w := httptest.NewRecorder()
		m.Set(w, "name", "value", 3600)

		// Extract cookie from response
		resp := w.Result()
		cookies := resp.Cookies()
		if len(cookies) != 1 {
			t.Fatalf("expected 1 cookie, got %d", len(cookies))
		}

		c := cookies[0]
		if c.Name != "name" || c.Value != "value" {
			t.Errorf("cookie = %s=%s, want name=value", c.Name, c.Value)
		}
		if c.MaxAge != 3600 {
			t.Errorf("MaxAge = %d, want 3600", c.MaxAge)
		}

		// Read the cookie back
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.AddCookie(c)

		val, err := m.Get(r, "name")
		if err != nil {
			t.Fatalf("Get() error: %v", err)
		}
		if val != "value" {
			t.Errorf("Get() = %q, want %q", val, "value")
		}
	})

	t.Run("delete cookie", func(t *testing.T) {
		w := httptest.NewRecorder()
		m.Delete(w, "name")

		resp := w.Result()
		cookies := resp.Cookies()
		if len(cookies) != 1 {
			t.Fatalf("expected 1 cookie, got %d", len(cookies))
		}

		c := cookies[0]
		if c.MaxAge != -1 {
			t.Errorf("MaxAge = %d, want -1", c.MaxAge)
		}
	})
}

func TestSignedCookies(t *testing.T) {
	t.Run("no secret returns error", func(t *testing.T) {
		m := cookie.New() // no secret
		w := httptest.NewRecorder()

		err := m.SetSigned(w, "session", "data", 3600)
		if !errors.Is(err, cookie.ErrNoSecret) {
			t.Errorf("SetSigned() error = %v, want ErrNoSecret", err)
		}

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		_, err = m.GetSigned(r, "session")
		if !errors.Is(err, cookie.ErrNoSecret) {
			t.Errorf("GetSigned() error = %v, want ErrNoSecret", err)
		}
	})

	t.Run("short secret is ignored", func(t *testing.T) {
		m := cookie.New(cookie.WithSecret("short")) // less than 32 bytes
		w := httptest.NewRecorder()

		err := m.SetSigned(w, "session", "data", 3600)
		if !errors.Is(err, cookie.ErrNoSecret) {
			t.Errorf("SetSigned() error = %v, want ErrNoSecret", err)
		}
	})

	t.Run("set and get signed cookie", func(t *testing.T) {
		m := cookie.New(cookie.WithSecret(testSecret))

		w := httptest.NewRecorder()
		if err := m.SetSigned(w, "session", "user123", 3600); err != nil {
			t.Fatalf("SetSigned() error: %v", err)
		}

		// Extract and re-read
		resp := w.Result()
		cookies := resp.Cookies()
		if len(cookies) != 1 {
			t.Fatalf("expected 1 cookie, got %d", len(cookies))
		}

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.AddCookie(cookies[0])

		val, err := m.GetSigned(r, "session")
		if err != nil {
			t.Fatalf("GetSigned() error: %v", err)
		}
		if val != "user123" {
			t.Errorf("GetSigned() = %q, want %q", val, "user123")
		}
	})

	t.Run("tampered cookie fails", func(t *testing.T) {
		m := cookie.New(cookie.WithSecret(testSecret))

		w := httptest.NewRecorder()
		_ = m.SetSigned(w, "session", "user123", 3600)

		resp := w.Result()
		c := resp.Cookies()[0]

		// Tamper with the value
		c.Value = "dGFtcGVyZWQ.invalid"

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.AddCookie(c)

		_, err := m.GetSigned(r, "session")
		if !errors.Is(err, cookie.ErrBadSig) {
			t.Errorf("GetSigned() error = %v, want ErrBadSig", err)
		}
	})

	t.Run("missing cookie returns not found", func(t *testing.T) {
		m := cookie.New(cookie.WithSecret(testSecret))
		r := httptest.NewRequest(http.MethodGet, "/", nil)

		_, err := m.GetSigned(r, "missing")
		if !errors.Is(err, cookie.ErrNotFound) {
			t.Errorf("GetSigned() error = %v, want ErrNotFound", err)
		}
	})
}

func TestEncryptedCookies(t *testing.T) {
	t.Run("no secret returns error", func(t *testing.T) {
		m := cookie.New() // no secret
		w := httptest.NewRecorder()

		err := m.SetEncrypted(w, "data", "secret", 3600)
		if !errors.Is(err, cookie.ErrNoSecret) {
			t.Errorf("SetEncrypted() error = %v, want ErrNoSecret", err)
		}

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		_, err = m.GetEncrypted(r, "data")
		if !errors.Is(err, cookie.ErrNoSecret) {
			t.Errorf("GetEncrypted() error = %v, want ErrNoSecret", err)
		}
	})

	t.Run("set and get encrypted cookie", func(t *testing.T) {
		m := cookie.New(cookie.WithSecret(testSecret))

		w := httptest.NewRecorder()
		if err := m.SetEncrypted(w, "secret", "confidential", 3600); err != nil {
			t.Fatalf("SetEncrypted() error: %v", err)
		}

		// Extract and re-read
		resp := w.Result()
		cookies := resp.Cookies()
		if len(cookies) != 1 {
			t.Fatalf("expected 1 cookie, got %d", len(cookies))
		}

		// Verify value is not plaintext
		if cookies[0].Value == "confidential" {
			t.Error("cookie value should be encrypted")
		}

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.AddCookie(cookies[0])

		val, err := m.GetEncrypted(r, "secret")
		if err != nil {
			t.Fatalf("GetEncrypted() error: %v", err)
		}
		if val != "confidential" {
			t.Errorf("GetEncrypted() = %q, want %q", val, "confidential")
		}
	})

	t.Run("tampered cookie fails", func(t *testing.T) {
		m := cookie.New(cookie.WithSecret(testSecret))

		w := httptest.NewRecorder()
		_ = m.SetEncrypted(w, "secret", "confidential", 3600)

		resp := w.Result()
		c := resp.Cookies()[0]

		// Tamper with the value
		c.Value = "tamperedvalue"

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.AddCookie(c)

		_, err := m.GetEncrypted(r, "secret")
		if !errors.Is(err, cookie.ErrDecrypt) {
			t.Errorf("GetEncrypted() error = %v, want ErrDecrypt", err)
		}
	})

	t.Run("missing cookie returns not found", func(t *testing.T) {
		m := cookie.New(cookie.WithSecret(testSecret))
		r := httptest.NewRequest(http.MethodGet, "/", nil)

		_, err := m.GetEncrypted(r, "missing")
		if !errors.Is(err, cookie.ErrNotFound) {
			t.Errorf("GetEncrypted() error = %v, want ErrNotFound", err)
		}
	})
}

func TestFlash(t *testing.T) {
	t.Run("no secret returns error", func(t *testing.T) {
		m := cookie.New()
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/", nil)

		err := m.SetFlash(w, "msg", "hello")
		if !errors.Is(err, cookie.ErrNoSecret) {
			t.Errorf("SetFlash() error = %v, want ErrNoSecret", err)
		}

		var dest string
		err = m.Flash(w, r, "msg", &dest)
		if !errors.Is(err, cookie.ErrNoSecret) {
			t.Errorf("Flash() error = %v, want ErrNoSecret", err)
		}
	})

	t.Run("set and get flash", func(t *testing.T) {
		m := cookie.New(cookie.WithSecret(testSecret))

		// Set flash
		w := httptest.NewRecorder()
		msg := map[string]string{"type": "success", "text": "Saved!"}
		if err := m.SetFlash(w, "msg", msg); err != nil {
			t.Fatalf("SetFlash() error: %v", err)
		}

		// Extract cookie
		resp := w.Result()
		cookies := resp.Cookies()
		if len(cookies) != 1 {
			t.Fatalf("expected 1 cookie, got %d", len(cookies))
		}

		// Verify cookie name has flash_ prefix
		if cookies[0].Name != "flash_msg" {
			t.Errorf("cookie name = %q, want %q", cookies[0].Name, "flash_msg")
		}

		// Read flash
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.AddCookie(cookies[0])

		w2 := httptest.NewRecorder()
		var dest map[string]string
		if err := m.Flash(w2, r, "msg", &dest); err != nil {
			t.Fatalf("Flash() error: %v", err)
		}

		if dest["type"] != "success" || dest["text"] != "Saved!" {
			t.Errorf("Flash() = %v, want %v", dest, msg)
		}

		// Verify flash was deleted
		resp2 := w2.Result()
		deleteCookies := resp2.Cookies()
		if len(deleteCookies) != 1 {
			t.Fatalf("expected 1 delete cookie, got %d", len(deleteCookies))
		}
		if deleteCookies[0].MaxAge != -1 {
			t.Errorf("flash cookie MaxAge = %d, want -1", deleteCookies[0].MaxAge)
		}
	})

	t.Run("missing flash returns not found", func(t *testing.T) {
		m := cookie.New(cookie.WithSecret(testSecret))
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/", nil)

		var dest string
		err := m.Flash(w, r, "missing", &dest)
		if !errors.Is(err, cookie.ErrNotFound) {
			t.Errorf("Flash() error = %v, want ErrNotFound", err)
		}
	})
}

func TestCookieAttributes(t *testing.T) {
	m := cookie.New(
		cookie.WithSecret(testSecret),
		cookie.WithDomain("example.com"),
		cookie.WithPath("/app"),
		cookie.WithSecure(true),
		cookie.WithHTTPOnly(true),
		cookie.WithSameSite(http.SameSiteStrictMode),
	)

	w := httptest.NewRecorder()
	m.Set(w, "test", "value", 3600)

	resp := w.Result()
	c := resp.Cookies()[0]

	if c.Domain != "example.com" {
		t.Errorf("Domain = %q, want %q", c.Domain, "example.com")
	}
	if c.Path != "/app" {
		t.Errorf("Path = %q, want %q", c.Path, "/app")
	}
	if !c.Secure {
		t.Error("Secure = false, want true")
	}
	if !c.HttpOnly {
		t.Error("HttpOnly = false, want true")
	}
	if c.SameSite != http.SameSiteStrictMode {
		t.Errorf("SameSite = %v, want %v", c.SameSite, http.SameSiteStrictMode)
	}
}

func TestDefaultAttributes(t *testing.T) {
	m := cookie.New()

	w := httptest.NewRecorder()
	m.Set(w, "test", "value", 3600)

	resp := w.Result()
	c := resp.Cookies()[0]

	if c.Path != "/" {
		t.Errorf("default Path = %q, want %q", c.Path, "/")
	}
	if !c.HttpOnly {
		t.Error("default HttpOnly = false, want true")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Errorf("default SameSite = %v, want %v", c.SameSite, http.SameSiteLaxMode)
	}
}
