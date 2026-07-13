package rng_test

import (
	"testing"

	"github.com/dmitrymomot/forge/gaming/rng"
)

func FuzzStream(f *testing.F) {
	f.Add([]byte("0123456789abcdef0123456789abcdef"), "alice", uint64(0), 6)
	f.Add([]byte("0123456789abcdef0123456789abcdef"), "Z_-9", uint64(1<<63), 1)
	f.Fuzz(func(t *testing.T, seed []byte, client string, nonce uint64, n int) {
		s, err := rng.New(seed, client, nonce)
		if err != nil {
			return // invalid inputs must error, never panic
		}
		if n <= 0 {
			n = 1
		}
		if v := s.IntN(n); v < 0 || v >= n {
			t.Fatalf("IntN(%d) = %d out of range", n, v)
		}
		if f64 := s.Float64(); f64 < 0 || f64 >= 1 {
			t.Fatalf("Float64() = %v out of range", f64)
		}
		s.Shuffle(8, func(i, j int) {})
	})
}
