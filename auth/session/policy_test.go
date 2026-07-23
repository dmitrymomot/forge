package session_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/dmitrymomot/forge/auth/session"
)

func TestDenyAndRevokeCarryReasons(t *testing.T) {
	deny := session.Deny("outside business hours")
	reason, ok := session.IsDeny(deny)
	if !ok {
		t.Fatal("IsDeny must recognize a Deny")
	}
	if reason != "outside business hours" {
		t.Fatalf("reason = %q, want %q", reason, "outside business hours")
	}
	if _, ok := session.IsRevoke(deny); ok {
		t.Fatal("a Deny must not be mistaken for a Revoke — the record survives a Deny")
	}

	revoke := session.Revoke("fingerprint drift")
	reason, ok = session.IsRevoke(revoke)
	if !ok {
		t.Fatal("IsRevoke must recognize a Revoke")
	}
	if reason != "fingerprint drift" {
		t.Fatalf("reason = %q, want %q", reason, "fingerprint drift")
	}
}

func TestDenyAndRevokeSurviveWrapping(t *testing.T) {
	wrapped := fmt.Errorf("policy chain: %w", session.Revoke("stolen"))
	if reason, ok := session.IsRevoke(wrapped); !ok || reason != "stolen" {
		t.Fatalf("IsRevoke through a wrap = (%q, %v), want (stolen, true)", reason, ok)
	}
}

func TestPlainErrorIsNeitherDenyNorRevoke(t *testing.T) {
	err := errors.New("database unreachable")
	if _, ok := session.IsDeny(err); ok {
		t.Fatal("an infrastructure error must not be treated as a Deny")
	}
	if _, ok := session.IsRevoke(err); ok {
		t.Fatal("an infrastructure error must not be treated as a Revoke")
	}
}
