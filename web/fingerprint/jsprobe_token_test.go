package fingerprint_test

import (
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/web/fingerprint"
)

func TestTokenRoundTrip(t *testing.T) {
	mock := clock.NewMock(time.Unix(1_700_000_000, 0))
	cfg := fingerprint.Config{Secret: "s", Version: 1, TokenTTL: time.Minute}
	fp, err := fingerprint.New(cfg, fingerprint.WithClock(mock))
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "203.0.113.7:5555"

	tok, err := fp.IssueToken(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := fp.VerifyTokenForTest(r, tok); err != nil {
		t.Fatalf("valid token rejected: %v", err)
	}
	// Tamper: flip a character.
	if err := fp.VerifyTokenForTest(r, tok+"x"); !errors.Is(err, fingerprint.ErrBadToken) {
		t.Fatalf("tamper not rejected: %v", err)
	}
	// Expiry.
	mock.Advance(2 * time.Minute)
	if err := fp.VerifyTokenForTest(r, tok); !errors.Is(err, fingerprint.ErrBadToken) {
		t.Fatalf("expired token accepted: %v", err)
	}
}
