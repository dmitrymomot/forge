package cookie_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/crypto/keyset"
	"github.com/dmitrymomot/forge/web/cookie"
)

func newCodec(t *testing.T, opts ...cookie.Option) *cookie.Codec {
	t.Helper()
	ks, err := keyset.New(keyset.WithPrimary(1, make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	c, err := cookie.New(ks, opts...)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// roundTrip writes via set, then builds a request carrying the response cookies.
func roundTrip(t *testing.T, set func(w http.ResponseWriter) error) *http.Request {
	t.Helper()
	rec := httptest.NewRecorder()
	if err := set(rec); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, ck := range rec.Result().Cookies() {
		r.AddCookie(ck)
	}
	return r
}

func TestPlainRoundTripCarriesPolicy(t *testing.T) {
	c := newCodec(t)
	rec := httptest.NewRecorder()
	if err := c.Set(rec, "theme", "dark"); err != nil {
		t.Fatal(err)
	}
	cks := rec.Result().Cookies()
	if len(cks) != 1 {
		t.Fatalf("want 1 cookie, got %d", len(cks))
	}
	ck := cks[0]
	if ck.Value != "dark" || !ck.Secure || !ck.HttpOnly || ck.Path != "/" || ck.SameSite != http.SameSiteLaxMode {
		t.Fatalf("policy flags not applied: %+v", ck)
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(ck)
	got, err := c.Get(r, "theme")
	if err != nil || got != "dark" {
		t.Fatalf("Get = %q, %v", got, err)
	}
}

func TestSignedRoundTrip(t *testing.T) {
	c := newCodec(t)
	r := roundTrip(t, func(w http.ResponseWriter) error { return c.SetSigned(w, "sid", "hello world") })
	got, err := c.GetSigned(r, "sid")
	if err != nil || got != "hello world" {
		t.Fatalf("GetSigned = %q, %v", got, err)
	}
}

func TestSignedTamperRejected(t *testing.T) {
	c := newCodec(t)
	rec := httptest.NewRecorder()
	if err := c.SetSigned(rec, "sid", "value"); err != nil {
		t.Fatal(err)
	}
	ck := rec.Result().Cookies()[0]
	ck.Value = strings.Replace(ck.Value, string(ck.Value[0]), "x", 1)
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(ck)
	if _, err := c.GetSigned(r, "sid"); !errors.Is(err, cookie.ErrInvalidCookie) {
		t.Fatalf("want ErrInvalidCookie, got %v", err)
	}
}

func TestSignedNameBindingRejectsReplay(t *testing.T) {
	c := newCodec(t)
	rec := httptest.NewRecorder()
	if err := c.SetSigned(rec, "a", "value"); err != nil {
		t.Fatal(err)
	}
	stolen := rec.Result().Cookies()[0]
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: "b", Value: stolen.Value})
	if _, err := c.GetSigned(r, "b"); !errors.Is(err, cookie.ErrInvalidCookie) {
		t.Fatalf("cookie minted for a must not verify as b, got %v", err)
	}
}

func TestEncryptedRoundTripAndOpacity(t *testing.T) {
	c := newCodec(t)
	rec := httptest.NewRecorder()
	if err := c.SetEncrypted(rec, "sess", "secret-data"); err != nil {
		t.Fatal(err)
	}
	ck := rec.Result().Cookies()[0]
	if strings.Contains(ck.Value, "secret-data") {
		t.Fatal("encrypted cookie leaks plaintext")
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(ck)
	got, err := c.GetEncrypted(r, "sess")
	if err != nil || got != "secret-data" {
		t.Fatalf("GetEncrypted = %q, %v", got, err)
	}
}

func TestEncryptedNameBindingRejectsReplay(t *testing.T) {
	c := newCodec(t)
	rec := httptest.NewRecorder()
	if err := c.SetEncrypted(rec, "a", "value"); err != nil {
		t.Fatal(err)
	}
	stolen := rec.Result().Cookies()[0]
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: "b", Value: stolen.Value})
	if _, err := c.GetEncrypted(r, "b"); !errors.Is(err, cookie.ErrInvalidCookie) {
		t.Fatalf("ciphertext minted for a must not decrypt as b, got %v", err)
	}
}

func TestRotationRetiredKeyStillReads(t *testing.T) {
	old := make([]byte, 32)
	newKey := make([]byte, 32)
	newKey[0] = 1
	ks1, _ := keyset.New(keyset.WithPrimary(1, old))
	c1, err := cookie.New(ks1)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	if err := c1.SetSigned(rec, "sid", "v"); err != nil {
		t.Fatal(err)
	}
	if err := c1.SetEncrypted(rec, "sess", "w"); err != nil {
		t.Fatal(err)
	}
	ks2, _ := keyset.New(keyset.WithPrimary(2, newKey), keyset.WithRetired(1, old))
	c2, err := cookie.New(ks2)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, ck := range rec.Result().Cookies() {
		r.AddCookie(ck)
	}
	if got, err := c2.GetSigned(r, "sid"); err != nil || got != "v" {
		t.Fatalf("rotated GetSigned = %q, %v", got, err)
	}
	if got, err := c2.GetEncrypted(r, "sess"); err != nil || got != "w" {
		t.Fatalf("rotated GetEncrypted = %q, %v", got, err)
	}
}

func TestMissingCookieIsInvalid(t *testing.T) {
	c := newCodec(t)
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, get := range []func() (string, error){
		func() (string, error) { return c.Get(r, "nope") },
		func() (string, error) { return c.GetSigned(r, "nope") },
		func() (string, error) { return c.GetEncrypted(r, "nope") },
	} {
		if _, err := get(); !errors.Is(err, cookie.ErrInvalidCookie) {
			t.Fatalf("want ErrInvalidCookie, got %v", err)
		}
	}
}

func TestHostPrefixEnforcement(t *testing.T) {
	c := newCodec(t, cookie.WithSecure(false))
	rec := httptest.NewRecorder()
	if err := c.SetSigned(rec, "__Host-x", "v"); !errors.Is(err, cookie.ErrInvalidConfig) {
		t.Fatalf("__Host- without Secure must fail, got %v", err)
	}
	if newCodec(t).SupportsHostPrefix() != true {
		t.Fatal("default policy should support __Host-")
	}
	if c.SupportsHostPrefix() {
		t.Fatal("insecure policy must not support __Host-")
	}
}

func TestTooLarge(t *testing.T) {
	c := newCodec(t)
	rec := httptest.NewRecorder()
	big := strings.Repeat("a", 5000)
	if err := c.Set(rec, "big", big); !errors.Is(err, cookie.ErrTooLarge) {
		t.Fatalf("want ErrTooLarge, got %v", err)
	}
}

func TestWriteOptionOverrides(t *testing.T) {
	c := newCodec(t)
	rec := httptest.NewRecorder()
	if err := c.Set(rec, "flash", "hi", cookie.WithWriteMaxAge(time.Minute), cookie.WithWritePath("/admin")); err != nil {
		t.Fatal(err)
	}
	ck := rec.Result().Cookies()[0]
	if ck.MaxAge != 60 || ck.Path != "/admin" {
		t.Fatalf("write overrides not applied: %+v", ck)
	}
}

func TestDeleteExpires(t *testing.T) {
	c := newCodec(t)
	rec := httptest.NewRecorder()
	c.Delete(rec, "sid")
	ck := rec.Result().Cookies()[0]
	if ck.MaxAge != -1 || ck.Value != "" {
		t.Fatalf("Delete must expire the cookie: %+v", ck)
	}
}
