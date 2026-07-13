package otp_test

import (
	"errors"
	"testing"

	"github.com/dmitrymomot/forge/auth/otp"
	"github.com/dmitrymomot/forge/resilience/cache"
)

func newBenchOTP(b *testing.B, opts ...otp.Option) *otp.OTP {
	b.Helper()
	store := cache.NewMemoryStore()
	b.Cleanup(func() { _ = store.Close() })
	o, err := otp.New([]byte("0123456789abcdef0123456789abcdef"), store, opts...)
	if err != nil {
		b.Fatal(err)
	}
	return o
}

func BenchmarkGenerate(b *testing.B) {
	o := newBenchOTP(b)
	ctx := b.Context()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := o.Generate(ctx, "user@example.com"); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkGenerateVerify measures the full happy cycle — the real-world
// unit of work is one verify per generate (verify is single-use).
func BenchmarkGenerateVerify(b *testing.B) {
	o := newBenchOTP(b)
	ctx := b.Context()
	b.ReportAllocs()
	for b.Loop() {
		code, err := o.Generate(ctx, "user@example.com")
		if err != nil {
			b.Fatal(err)
		}
		if err := o.Verify(ctx, "user@example.com", code); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkVerifyMismatch(b *testing.B) {
	o := newBenchOTP(b, otp.WithMaxAttempts(255))
	ctx := b.Context()
	if _, err := o.Generate(ctx, "user@example.com"); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		err := o.Verify(ctx, "user@example.com", "999999999") // wrong length: can never match
		if errors.Is(err, otp.ErrTooManyAttempts) || errors.Is(err, otp.ErrNotFound) {
			if _, err := o.Generate(ctx, "user@example.com"); err != nil {
				b.Fatal(err)
			}
			continue
		}
		if !errors.Is(err, otp.ErrCodeMismatch) {
			b.Fatal(err)
		}
	}
}
