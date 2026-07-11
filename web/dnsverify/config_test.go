package dnsverify_test

import (
	"errors"
	"strings"
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

func TestConfigValidateLabelSyntax(t *testing.T) {
	base := dnsverify.DefaultConfig()

	valid := []string{
		"_forge-verify", // default, underscore-prefixed service label
		"_custom",
		"verify",
		"a",
		"_acme-challenge",
		"_forge-verify.sub", // dotted prefix (each part a valid label)
	}
	for _, label := range valid {
		c := base
		c.Label = label
		if err := c.Validate(); err != nil {
			t.Errorf("Label %q must be valid, got %v", label, err)
		}
	}

	invalid := []string{
		"",                      // empty
		"_forge verify",         // space
		"_forge-verify.",        // trailing dot → empty final label
		".verify",               // leading dot → empty first label
		"a..b",                  // consecutive dots → empty middle label
		"-bad",                  // leading hyphen
		"bad-",                  // trailing hyphen
		"has_underscore ",       // trailing space
		"bad!",                  // illegal punctuation
		"café",                  // non-ASCII
		strings.Repeat("a", 64), // label longer than 63 chars
	}
	for _, label := range invalid {
		c := base
		c.Label = label
		if err := c.Validate(); !errors.Is(err, dnsverify.ErrInvalidConfig) {
			t.Errorf("Label %q must be invalid (ErrInvalidConfig), got %v", label, err)
		}
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
