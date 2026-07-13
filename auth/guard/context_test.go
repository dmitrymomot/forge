package guard_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/dmitrymomot/forge/auth/guard"
)

func TestFrom_Absent(t *testing.T) {
	t.Parallel()
	id, ok := guard.From(context.Background())
	if ok {
		t.Fatalf("From on empty ctx: ok = true, want false (id=%+v)", id)
	}
}

func TestMustFrom_PanicsWhenAbsent(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Fatal("MustFrom on empty ctx did not panic")
		}
	}()
	guard.MustFrom(context.Background())
}

func TestVerifierFunc_ImplementsVerifier(t *testing.T) {
	t.Parallel()
	want := guard.Identity{Subject: "u1", Method: "test"}
	var v guard.Verifier = guard.VerifierFunc(func(_ context.Context, cred string) (guard.Identity, error) {
		if cred != "tok" {
			t.Fatalf("credential = %q, want %q", cred, "tok")
		}
		return want, nil
	})
	got, err := v.Verify(context.Background(), "tok")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got.Subject != want.Subject || got.Method != want.Method {
		t.Fatalf("Verify = %+v, want %+v", got, want)
	}
}

func TestSentinels_AreDistinct(t *testing.T) {
	t.Parallel()
	if errors.Is(guard.ErrNoCredential, guard.ErrInvalidCredential) {
		t.Fatal("ErrNoCredential must not match ErrInvalidCredential")
	}
}

func TestLogExtractor_NoIdentity(t *testing.T) {
	t.Parallel()
	if _, ok := guard.LogExtractor(context.Background()); ok {
		t.Fatal("LogExtractor on empty ctx: ok = true, want false")
	}
}

func TestLogExtractor_SubjectOnly(t *testing.T) {
	t.Parallel()
	// There is no exported setter (only the middleware stores identities), so
	// this test goes through the middleware in Task 3. For now assert only the
	// empty-context contract above; this test body is extended in Task 3.
	t.Skip("extended in Task 3 once New can store an Identity")
	_ = slog.Attr{}
}
