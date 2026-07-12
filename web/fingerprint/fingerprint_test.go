package fingerprint_test

import (
	"slices"
	"testing"

	"github.com/dmitrymomot/forge/web/fingerprint"
)

func TestDrift(t *testing.T) {
	old := fingerprint.Digest{Version: 1, Parts: map[string]string{"ua": "a", "ip": "b"}}
	next := fingerprint.Digest{Version: 1, Parts: map[string]string{"ua": "a", "ip": "c", "tls": "d"}}
	got := fingerprint.Drift(old, next)
	if want := []string{"ip", "tls"}; !slices.Equal(got, want) {
		t.Fatalf("Drift = %v, want %v", got, want)
	}
	if d := fingerprint.Drift(old, old); len(d) != 0 {
		t.Fatalf("no drift expected, got %v", d)
	}
}
