package fingerprint_test

import (
	"errors"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/web/fingerprint"
)

func TestConfigValidate(t *testing.T) {
	if err := (fingerprint.Config{Version: 1, TokenTTL: time.Minute}).Validate(); !errors.Is(err, fingerprint.ErrNoSecret) {
		t.Fatalf("missing secret: got %v", err)
	}
	if err := (fingerprint.Config{Secret: "s", Version: 0, TokenTTL: time.Minute}).Validate(); !errors.Is(err, fingerprint.ErrBadVersion) {
		t.Fatalf("bad version: got %v", err)
	}
	if err := (fingerprint.Config{Secret: "s", Version: 1, TokenTTL: 0}).Validate(); !errors.Is(err, fingerprint.ErrBadTokenTTL) {
		t.Fatalf("bad ttl: got %v", err)
	}
	if cfg := fingerprint.DefaultConfig(); cfg.Version != 1 || cfg.TokenTTL <= 0 {
		t.Fatalf("defaults: %+v", cfg)
	}
	ok := fingerprint.Config{Secret: "s", Version: 1, TokenTTL: time.Minute}
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
}
