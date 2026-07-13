package otp_test

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/auth/otp"
	"github.com/dmitrymomot/forge/core/clock"
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

// ttlSpyStore wraps a cache.Store and records the effective TTL passed to
// the most recent Set call, so tests can assert on the TTL argument itself
// rather than on effects that could pass for other reasons.
type ttlSpyStore struct {
	cache.Store
	mu       sync.Mutex
	lastTTL  time.Duration
	setCalls int
}

func (s *ttlSpyStore) Set(ctx context.Context, key string, val []byte, opts ...cache.SetOption) error {
	resolved := cache.ApplySetOptions(opts...)
	s.mu.Lock()
	s.lastTTL = resolved.TTL
	s.setCalls++
	s.mu.Unlock()
	return s.Store.Set(ctx, key, val, opts...)
}

func (s *ttlSpyStore) LastTTL() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastTTL
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

var baseTime = time.Unix(1_700_000_000, 0)

// wrongCode returns a code of the same length guaranteed != code.
func wrongCode(code string) string {
	b := []byte(code)
	if b[0] == '0' {
		b[0] = '1'
	} else {
		b[0] = '0'
	}
	return string(b)
}

func TestVerify_HappyPathSingleUse(t *testing.T) {
	t.Parallel()
	o, err := otp.New(testSecret, newStore(t))
	if err != nil {
		t.Fatal(err)
	}
	code, err := o.Generate(t.Context(), "user@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if err := o.Verify(t.Context(), "user@example.com", code); err != nil {
		t.Fatalf("Verify correct code: %v", err)
	}
	if err := o.Verify(t.Context(), "user@example.com", code); !errors.Is(err, otp.ErrNotFound) {
		t.Fatalf("second Verify = %v, want ErrNotFound (single-use)", err)
	}
}

func TestVerify_NeverIssued(t *testing.T) {
	t.Parallel()
	o, err := otp.New(testSecret, newStore(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := o.Verify(t.Context(), "user@example.com", "123456"); !errors.Is(err, otp.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestVerify_AttemptExhaustion(t *testing.T) {
	t.Parallel()
	o, err := otp.New(testSecret, newStore(t), otp.WithMaxAttempts(3))
	if err != nil {
		t.Fatal(err)
	}
	code, err := o.Generate(t.Context(), "user@example.com")
	if err != nil {
		t.Fatal(err)
	}
	bad := wrongCode(code)

	// Attempts 1 and 2: mismatch with attempts remaining.
	for i := range 2 {
		if err := o.Verify(t.Context(), "user@example.com", bad); !errors.Is(err, otp.ErrCodeMismatch) {
			t.Fatalf("attempt %d: err = %v, want ErrCodeMismatch", i+1, err)
		}
	}
	// Attempt 3 consumes the limit.
	if err := o.Verify(t.Context(), "user@example.com", bad); !errors.Is(err, otp.ErrTooManyAttempts) {
		t.Fatalf("limit-consuming attempt: err = %v, want ErrTooManyAttempts", err)
	}
	// The code is dead even for the CORRECT value.
	if err := o.Verify(t.Context(), "user@example.com", code); !errors.Is(err, otp.ErrNotFound) {
		t.Fatalf("correct code after exhaustion: err = %v, want ErrNotFound", err)
	}
}

func TestVerify_Expiry(t *testing.T) {
	t.Parallel()
	clk := clock.NewMock(baseTime)
	o, err := otp.New(testSecret, newStore(t), otp.WithTTL(10*time.Minute), otp.WithClock(clk))
	if err != nil {
		t.Fatal(err)
	}
	code, err := o.Generate(t.Context(), "user@example.com")
	if err != nil {
		t.Fatal(err)
	}
	clk.Advance(10*time.Minute + time.Second)
	if err := o.Verify(t.Context(), "user@example.com", code); !errors.Is(err, otp.ErrNotFound) {
		t.Fatalf("expired code: err = %v, want ErrNotFound", err)
	}
}

func TestVerify_FailedAttemptDoesNotExtendLife(t *testing.T) {
	t.Parallel()
	clk := clock.NewMock(baseTime)
	o, err := otp.New(testSecret, newStore(t), otp.WithTTL(10*time.Minute), otp.WithClock(clk))
	if err != nil {
		t.Fatal(err)
	}
	code, err := o.Generate(t.Context(), "user@example.com")
	if err != nil {
		t.Fatal(err)
	}
	// A wrong attempt at minute 9 rewrites the record...
	clk.Advance(9 * time.Minute)
	if err := o.Verify(t.Context(), "user@example.com", wrongCode(code)); !errors.Is(err, otp.ErrCodeMismatch) {
		t.Fatal(err)
	}
	// ...but the code still dies at the ORIGINAL deadline.
	clk.Advance(time.Minute + time.Second)
	if err := o.Verify(t.Context(), "user@example.com", code); !errors.Is(err, otp.ErrNotFound) {
		t.Fatalf("code outlived its original deadline: err = %v, want ErrNotFound", err)
	}
}

func TestVerify_ReplaceOnGenerate(t *testing.T) {
	t.Parallel()
	o, err := otp.New(testSecret, newStore(t))
	if err != nil {
		t.Fatal(err)
	}
	code1, err := o.Generate(t.Context(), "user@example.com")
	if err != nil {
		t.Fatal(err)
	}
	code2, err := o.Generate(t.Context(), "user@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if code1 != code2 {
		// Old code is dead (mismatch consumes an attempt against code2's record).
		if err := o.Verify(t.Context(), "user@example.com", code1); !errors.Is(err, otp.ErrCodeMismatch) {
			t.Fatalf("old code: err = %v, want ErrCodeMismatch", err)
		}
	}
	if err := o.Verify(t.Context(), "user@example.com", code2); err != nil {
		t.Fatalf("new code: %v", err)
	}
}

func TestVerify_CorruptRecord(t *testing.T) {
	t.Parallel()
	store := newStore(t)
	o, err := otp.New(testSecret, store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := o.Generate(t.Context(), "user@example.com"); err != nil {
		t.Fatal(err)
	}
	key := storageKey("default", "", "user@example.com")
	// Unknown version byte reads as absent (forward-compat / revoked).
	if err := store.Set(t.Context(), key, []byte{0xFF, 0, 0, 0}); err != nil {
		t.Fatal(err)
	}
	if err := o.Verify(t.Context(), "user@example.com", "123456"); !errors.Is(err, otp.ErrNotFound) {
		t.Fatalf("corrupt record: err = %v, want ErrNotFound", err)
	}
}

func TestVerify_TenantIsolation(t *testing.T) {
	t.Parallel()
	tenantScope := func(ctx context.Context) (string, error) {
		s, _ := ctx.Value(ctxTenant{}).(string)
		return s, nil
	}
	o, err := otp.New(testSecret, newStore(t), otp.WithScope(tenantScope))
	if err != nil {
		t.Fatal(err)
	}
	ctxA := context.WithValue(t.Context(), ctxTenant{}, "tenant-a")
	ctxB := context.WithValue(t.Context(), ctxTenant{}, "tenant-b")

	code, err := o.Generate(ctxA, "user@example.com")
	if err != nil {
		t.Fatal(err)
	}
	// Tenant B never sees tenant A's code — not even as a mismatch.
	if err := o.Verify(ctxB, "user@example.com", code); !errors.Is(err, otp.ErrNotFound) {
		t.Fatalf("cross-tenant verify: err = %v, want ErrNotFound", err)
	}
	// Missing tenant in ctx fails closed, not into a global bucket.
	if err := o.Verify(t.Context(), "user@example.com", code); !errors.Is(err, otp.ErrScope) {
		t.Fatalf("scopeless ctx: err = %v, want ErrScope", err)
	}
	if err := o.Verify(ctxA, "user@example.com", code); err != nil {
		t.Fatalf("same-tenant verify: %v", err)
	}
}

func TestVerify_PurposeIsolation(t *testing.T) {
	t.Parallel()
	store := newStore(t)
	login, err := otp.New(testSecret, store, otp.WithPurpose("login"))
	if err != nil {
		t.Fatal(err)
	}
	reset, err := otp.New(testSecret, store, otp.WithPurpose("password-reset"))
	if err != nil {
		t.Fatal(err)
	}
	code, err := login.Generate(t.Context(), "user@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if err := reset.Verify(t.Context(), "user@example.com", code); !errors.Is(err, otp.ErrNotFound) {
		t.Fatalf("cross-purpose verify: err = %v, want ErrNotFound", err)
	}
}

func TestVerify_MismatchRewriteUsesRemainingTTL(t *testing.T) {
	t.Parallel()
	clk := clock.NewMock(baseTime)
	spy := &ttlSpyStore{Store: cache.NewMemoryStore()}
	t.Cleanup(func() { _ = spy.Close() })
	o, err := otp.New(testSecret, spy, otp.WithClock(clk), otp.WithTTL(10*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	code, err := o.Generate(t.Context(), "user@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if got := spy.LastTTL(); got != 10*time.Minute {
		t.Fatalf("Generate TTL = %v, want 10m", got)
	}

	clk.Advance(4 * time.Minute)
	if err := o.Verify(t.Context(), "user@example.com", wrongCode(code)); !errors.Is(err, otp.ErrCodeMismatch) {
		t.Fatalf("err = %v, want ErrCodeMismatch", err)
	}

	got := spy.LastTTL()
	if got != 6*time.Minute {
		t.Fatalf("mismatch-rewrite TTL = %v, want exactly 6m (remaining time), not the full ttl", got)
	}
	if got >= 10*time.Minute {
		t.Fatalf("mismatch-rewrite TTL = %v, must be strictly less than the full 10m ttl", got)
	}
}

func TestVerify_WrongVersionByte(t *testing.T) {
	t.Parallel()
	store := newStore(t)
	o, err := otp.New(testSecret, store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := o.Generate(t.Context(), "user@example.com"); err != nil {
		t.Fatal(err)
	}
	key := storageKey("default", "", "user@example.com")
	// Correct length (42 bytes), wrong version byte: exercises the
	// wrong-version-at-correct-length branch of decodeRecord, distinct from
	// the too-short-to-parse case in TestVerify_CorruptRecord.
	bad := make([]byte, 42)
	bad[0] = 0xFF
	if err := store.Set(t.Context(), key, bad); err != nil {
		t.Fatal(err)
	}
	if err := o.Verify(t.Context(), "user@example.com", "123456"); !errors.Is(err, otp.ErrNotFound) {
		t.Fatalf("wrong-version record: err = %v, want ErrNotFound", err)
	}
}

func TestRevoke(t *testing.T) {
	t.Parallel()
	o, err := otp.New(testSecret, newStore(t))
	if err != nil {
		t.Fatal(err)
	}
	code, err := o.Generate(t.Context(), "user@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if err := o.Revoke(t.Context(), "user@example.com"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if err := o.Verify(t.Context(), "user@example.com", code); !errors.Is(err, otp.ErrNotFound) {
		t.Fatalf("revoked code: err = %v, want ErrNotFound", err)
	}
	// Revoking with nothing outstanding is a no-op, not an error.
	if err := o.Revoke(t.Context(), "user@example.com"); err != nil {
		t.Fatalf("idempotent Revoke: %v", err)
	}
}

// TestVerify_ConcurrentWrongGuesses exercises the documented read-modify-write
// attempt counter under the race detector. The limit may be overshot by the
// in-flight count (documented caveat), but a wrong code must NEVER verify.
func TestVerify_ConcurrentWrongGuesses(t *testing.T) {
	t.Parallel()
	o, err := otp.New(testSecret, newStore(t), otp.WithMaxAttempts(3))
	if err != nil {
		t.Fatal(err)
	}
	code, err := o.Generate(t.Context(), "user@example.com")
	if err != nil {
		t.Fatal(err)
	}
	bad := wrongCode(code)

	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			if err := o.Verify(t.Context(), "user@example.com", bad); err == nil {
				t.Error("wrong code verified")
			}
		})
	}
	wg.Wait()
}
