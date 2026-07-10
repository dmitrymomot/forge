package ipfilter_test

import (
	"errors"
	"testing"

	"github.com/dmitrymomot/forge/web/ipfilter"
)

func TestInvalidAllowCIDRPanics(t *testing.T) {
	assertPanicsWithErrInvalidCIDR(t, func() {
		ipfilter.New(ipfilter.WithAllow("not-a-cidr"))
	})
}

func TestInvalidDenyCIDRPanics(t *testing.T) {
	assertPanicsWithErrInvalidCIDR(t, func() {
		ipfilter.New(ipfilter.WithDeny("999.999.0.0/16"))
	})
}

func assertPanicsWithErrInvalidCIDR(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic, got none")
		}
		err, ok := r.(error)
		if !ok || !errors.Is(err, ipfilter.ErrInvalidCIDR) {
			t.Fatalf("want ErrInvalidCIDR, got %v", r)
		}
	}()
	fn()
}
