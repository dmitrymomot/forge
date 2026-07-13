package lockout

import (
	"testing"
	"time"
)

func TestLockDurationClampsOverflow(t *testing.T) {
	t.Parallel()
	l := &Locker{cfg: config{
		threshold: 1,
		baseLock:  time.Minute,
		factor:    2,
		maxLock:   15 * time.Minute,
	}}
	tests := []struct {
		n    int64
		want time.Duration
	}{
		{2, time.Minute},            // 2^0
		{3, 2 * time.Minute},        // 2^1
		{5, 8 * time.Minute},        // 2^3
		{6, 15 * time.Minute},       // 16m → cap
		{100, 15 * time.Minute},     // huge exponent → cap, no overflow
		{1 << 62, 15 * time.Minute}, // float Inf → cap, no panic
	}
	for _, tt := range tests {
		if got := l.lockDuration(tt.n); got != tt.want {
			t.Errorf("lockDuration(%d) = %v, want %v", tt.n, got, tt.want)
		}
	}
}
