package cookie_test

import (
	"encoding/base64"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/dmitrymomot/forge/web/cookie"
)

func TestFromConfigRoundTrip(t *testing.T) {
	keys := "1:" + base64.StdEncoding.EncodeToString(make([]byte, 32))
	cfg := cookie.DefaultConfig()
	cfg.Keys = keys
	c, err := cookie.FromConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	if err := c.SetSigned(rec, "sid", "v"); err != nil {
		t.Fatal(err)
	}
}

func TestConfigValidate(t *testing.T) {
	cfg := cookie.DefaultConfig()
	cfg.SameSite = "bogus"
	if err := cfg.Validate(); !errors.Is(err, cookie.ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig, got %v", err)
	}
	none := cookie.DefaultConfig()
	none.SameSite = "none"
	none.Secure = false
	if err := none.Validate(); !errors.Is(err, cookie.ErrInvalidConfig) {
		t.Fatalf("SameSite=none without Secure must fail, got %v", err)
	}
}
