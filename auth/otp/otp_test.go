package otp_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/auth/otp"
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
