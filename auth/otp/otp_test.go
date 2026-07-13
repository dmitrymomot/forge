package otp_test

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/auth/otp"
	"github.com/dmitrymomot/forge/crypto/digest"
	"github.com/dmitrymomot/forge/resilience/cache"
)

var testSecret = []byte("0123456789abcdef")

func newStore(t *testing.T) cache.Store {
	t.Helper()
	store := cache.NewMemoryStore()
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestNew_Valid(t *testing.T) {
	t.Parallel()
	o, err := otp.New(testSecret, newStore(t))
	if err != nil {
		t.Fatalf("New with defaults: %v", err)
	}
	if o == nil {
		t.Fatal("New returned nil *OTP")
	}
}

func TestNew_InvalidConfig(t *testing.T) {
	t.Parallel()
	store := newStore(t)
	tests := []struct {
		name   string
		secret []byte
		store  cache.Store
		opts   []otp.Option
	}{
		{"short secret", []byte("tooshort"), store, nil},
		{"nil secret", nil, store, nil},
		{"nil store", testSecret, nil, nil},
		{"empty purpose", testSecret, store, []otp.Option{otp.WithPurpose("")}},
		{"zero ttl", testSecret, store, []otp.Option{otp.WithTTL(0)}},
		{"negative ttl", testSecret, store, []otp.Option{otp.WithTTL(-time.Minute)}},
		{"length too short", testSecret, store, []otp.Option{otp.WithLength(3)}},
		{"length too long", testSecret, store, []otp.Option{otp.WithLength(11)}},
		{"zero attempts", testSecret, store, []otp.Option{otp.WithMaxAttempts(0)}},
		{"attempts over byte", testSecret, store, []otp.Option{otp.WithMaxAttempts(256)}},
		{"nil clock", testSecret, store, []otp.Option{otp.WithClock(nil)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := otp.New(tt.secret, tt.store, tt.opts...); !errors.Is(err, otp.ErrInvalidConfig) {
				t.Fatalf("err = %v, want ErrInvalidConfig", err)
			}
		})
	}
}

func TestNew_BoundaryValues(t *testing.T) {
	t.Parallel()
	opts := []otp.Option{
		otp.WithLength(4), otp.WithMaxAttempts(255), otp.WithTTL(time.Second),
		otp.WithScope(func(context.Context) (string, error) { return "t1", nil }),
	}
	if _, err := otp.New(testSecret, newStore(t), opts...); err != nil {
		t.Fatalf("boundary values rejected: %v", err)
	}
	if _, err := otp.New(testSecret, newStore(t), otp.WithLength(10)); err != nil {
		t.Fatalf("length 10 rejected: %v", err)
	}
}

// storageKey replicates the documented storage-key contract so tests can
// inspect raw records black-box.
func storageKey(purpose, scope, identifier string) string {
	buf := make([]byte, 0, 8+len(scope)+len(identifier))
	buf = binary.BigEndian.AppendUint32(buf, uint32(len(scope)))
	buf = append(buf, scope...)
	buf = binary.BigEndian.AppendUint32(buf, uint32(len(identifier)))
	buf = append(buf, identifier...)
	return "otp:" + purpose + ":" + hex.EncodeToString(digest.SHA256(buf))
}

func TestGenerate_CodeFormat(t *testing.T) {
	t.Parallel()
	o, err := otp.New(testSecret, newStore(t))
	if err != nil {
		t.Fatal(err)
	}
	code, err := o.Generate(t.Context(), "user@example.com")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(code) != 6 {
		t.Fatalf("code length = %d, want 6", len(code))
	}
	for _, r := range code {
		if r < '0' || r > '9' {
			t.Fatalf("code %q contains non-digit %q", code, r)
		}
	}
}

func TestGenerate_HashedAtRest(t *testing.T) {
	t.Parallel()
	store := newStore(t)
	o, err := otp.New(testSecret, store, otp.WithPurpose("login"), otp.WithLength(8))
	if err != nil {
		t.Fatal(err)
	}
	code, err := o.Generate(t.Context(), "user@example.com")
	if err != nil {
		t.Fatal(err)
	}

	raw, err := store.Get(t.Context(), storageKey("login", "", "user@example.com"))
	if err != nil {
		t.Fatalf("record not at documented key: %v", err)
	}
	if len(raw) != 42 {
		t.Fatalf("record size = %d, want 42", len(raw))
	}
	if raw[0] != 0x01 {
		t.Fatalf("version byte = %#x, want 0x01", raw[0])
	}
	if raw[1] != 0 {
		t.Fatalf("initial attempts = %d, want 0", raw[1])
	}
	if strings.Contains(string(raw), code) {
		t.Fatal("plaintext code stored in record")
	}
	wantHash := digest.HMACSHA256(testSecret, []byte(code))
	if string(raw[10:]) != string(wantHash) {
		t.Fatal("stored hash is not HMAC-SHA256(secret, code)")
	}
}

func TestGenerate_ScopeSeparatesKeys(t *testing.T) {
	t.Parallel()
	store := newStore(t)
	tenantScope := func(ctx context.Context) (string, error) {
		s, _ := ctx.Value(ctxTenant{}).(string)
		return s, nil
	}
	o, err := otp.New(testSecret, store, otp.WithScope(tenantScope))
	if err != nil {
		t.Fatal(err)
	}
	ctxA := context.WithValue(t.Context(), ctxTenant{}, "tenant-a")
	if _, err := o.Generate(ctxA, "user@example.com"); err != nil {
		t.Fatal(err)
	}

	if _, err := store.Get(t.Context(), storageKey("default", "tenant-a", "user@example.com")); err != nil {
		t.Fatalf("record not under tenant-scoped key: %v", err)
	}
	// Delimiter games cannot collide: ("a:b","c") vs ("a","b:c").
	if storageKey("default", "a:b", "c") == storageKey("default", "a", "b:c") {
		t.Fatal("length-prefixed key derivation collided")
	}
}

type ctxTenant struct{}

func TestGenerate_ScopeFailClosed(t *testing.T) {
	t.Parallel()
	t.Run("hook error", func(t *testing.T) {
		t.Parallel()
		o, err := otp.New(testSecret, newStore(t),
			otp.WithScope(func(context.Context) (string, error) { return "", errors.New("no tenant") }))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := o.Generate(t.Context(), "user@example.com"); !errors.Is(err, otp.ErrScope) {
			t.Fatalf("err = %v, want ErrScope", err)
		}
	})
	t.Run("empty scope", func(t *testing.T) {
		t.Parallel()
		o, err := otp.New(testSecret, newStore(t),
			otp.WithScope(func(context.Context) (string, error) { return "", nil }))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := o.Generate(t.Context(), "user@example.com"); !errors.Is(err, otp.ErrScope) {
			t.Fatalf("err = %v, want ErrScope", err)
		}
	})
}
