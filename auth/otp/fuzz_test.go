package otp_test

import (
	"errors"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/dmitrymomot/forge/auth/otp"
	"github.com/dmitrymomot/forge/resilience/cache"
)

// FuzzVerify feeds arbitrary submitted codes through a full generate/verify
// cycle: only the exact issued code may verify; everything else must return
// ErrCodeMismatch, and nothing may panic.
func FuzzVerify(f *testing.F) {
	store := cache.NewMemoryStore()
	f.Cleanup(func() { _ = store.Close() })
	o, err := otp.New([]byte("0123456789abcdef"), store)
	if err != nil {
		f.Fatal(err)
	}

	f.Add("123456")
	f.Add("")
	f.Add("000000")
	f.Add("abcdef")
	f.Add("12345678901234567890")
	f.Add("\x00\x01\x02")

	var n atomic.Int64
	f.Fuzz(func(t *testing.T, submitted string) {
		identifier := fmt.Sprintf("user-%d@example.com", n.Add(1))
		ctx := t.Context()
		want, err := o.Generate(ctx, identifier)
		if err != nil {
			t.Fatal(err)
		}
		err = o.Verify(ctx, identifier, submitted)
		if submitted == want {
			if err != nil {
				t.Fatalf("exact code rejected: %v", err)
			}
			return
		}
		if !errors.Is(err, otp.ErrCodeMismatch) {
			t.Fatalf("wrong code %q: err = %v, want ErrCodeMismatch", submitted, err)
		}
	})
}
