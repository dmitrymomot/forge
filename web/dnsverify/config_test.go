package dnsverify_test

import (
	"errors"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/web/dnsverify"
)

func TestDefaultConfig(t *testing.T) {
	c := dnsverify.DefaultConfig()
	if c.Timeout != 5*time.Second || c.Label != "_forge-verify" || c.TokenBytes != 16 {
		t.Fatalf("DefaultConfig = %+v", c)
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("DefaultConfig must be valid: %v", err)
	}
}

func TestConfigValidate(t *testing.T) {
	base := dnsverify.DefaultConfig()

	bad := base
	bad.Timeout = 0
	if !errors.Is(bad.Validate(), dnsverify.ErrInvalidConfig) {
		t.Error("zero Timeout must be invalid")
	}

	bad = base
	bad.Label = ""
	if !errors.Is(bad.Validate(), dnsverify.ErrInvalidConfig) {
		t.Error("empty Label must be invalid")
	}

	bad = base
	bad.TokenBytes = 4
	if !errors.Is(bad.Validate(), dnsverify.ErrInvalidConfig) {
		t.Error("TokenBytes < 8 must be invalid")
	}
}

func TestNewRejectsInvalidConfig(t *testing.T) {
	_, err := dnsverify.New(dnsverify.WithTimeout(0))
	if !errors.Is(err, dnsverify.ErrInvalidConfig) {
		t.Fatalf("New with zero Timeout: want ErrInvalidConfig, got %v", err)
	}
}

func TestNewAppliesOptions(t *testing.T) {
	v, err := dnsverify.New(
		dnsverify.WithResolver(dnsverify.NewStaticResolver()),
		dnsverify.WithLabel("_custom"),
		dnsverify.WithTokenBytes(24),
	)
	if err != nil || v == nil {
		t.Fatalf("New: %v", err)
	}
}
