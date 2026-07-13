package guard_test

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestLogExtractor_ThroughMiddleware(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		id     guard.Identity
		tenant bool
	}{
		{"subject only", guard.Identity{Subject: "u1", Method: "test"}, false},
		{"subject and tenant", guard.Identity{Subject: "u1", Tenant: "t1", Method: "test"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			v := guard.VerifierFunc(func(context.Context, string) (guard.Identity, error) {
				return tt.id, nil
			})
			var attr slog.Attr
			var ok bool
			inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				attr, ok = guard.LogExtractor(r.Context())
			})
			h := guard.New(v)(inner)
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.Header.Set("Authorization", "Bearer tok")
			h.ServeHTTP(httptest.NewRecorder(), r)

			if !ok {
				t.Fatal("LogExtractor: ok = false behind the guard")
			}
			if attr.Key != "auth" {
				t.Fatalf("attr key = %q, want auth", attr.Key)
			}
			s := attr.Value.String()
			if !strings.Contains(s, "u1") {
				t.Fatalf("attr %q does not contain subject", s)
			}
			if tt.tenant != strings.Contains(s, "t1") {
				t.Fatalf("attr %q tenant presence, want %v", s, tt.tenant)
			}
		})
	}
}
