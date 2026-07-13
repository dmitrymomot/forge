package totp_test

import (
	"testing"
	"time"

	"github.com/dmitrymomot/forge/auth/totp"
	"github.com/dmitrymomot/forge/core/clock"
)

func BenchmarkCode(b *testing.B) {
	tp, err := totp.New()
	if err != nil {
		b.Fatal(err)
	}
	secret := "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	at := time.Unix(1_700_000_000, 0)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := tp.Code(secret, at); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkVerify(b *testing.B) {
	now := time.Unix(1_700_000_000, 0).UTC()
	tp, err := totp.New(totp.WithClock(clock.NewMock(now)))
	if err != nil {
		b.Fatal(err)
	}
	secret := "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	code, err := tp.Code(secret, now)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := tp.Verify(secret, code, time.Time{}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkManagerVerify(b *testing.B) {
	now := time.Unix(1_700_000_000, 0).UTC()
	store := totp.NewMemoryStore()
	key := []byte("0123456789abcdef0123456789abcdef")
	mgr, err := totp.NewManager(store, key, totp.WithClock(clock.NewMock(now)))
	if err != nil {
		b.Fatal(err)
	}
	ctx := b.Context()
	enr, err := mgr.BeginEnroll(ctx, "alice", "alice@acme.com")
	if err != nil {
		b.Fatal(err)
	}
	tp, err := totp.New()
	if err != nil {
		b.Fatal(err)
	}
	first, err := tp.Code(enr.Secret, now)
	if err != nil {
		b.Fatal(err)
	}
	if _, err := mgr.ConfirmEnroll(ctx, "alice", first); err != nil {
		b.Fatal(err)
	}
	// Bench the dominant path: a wrong TOTP code falling through the full
	// window scan and the backup-hash scan (verify successes mutate state
	// and cannot repeat in a loop).
	b.ReportAllocs()
	for b.Loop() {
		if _, err := mgr.Verify(ctx, "alice", "000000"); err == nil {
			b.Fatal("expected mismatch")
		}
	}
}

func BenchmarkVerifyBackupCode(b *testing.B) {
	codes, hashes, err := totp.GenerateBackupCodes(10, 10)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, ok := totp.VerifyBackupCode(codes[9], hashes); !ok {
			b.Fatal("expected match")
		}
	}
}
