package qrcode

import "testing"

func TestGFTables(t *testing.T) {
	// Anchors from GF(256) with primitive poly 0x11D, generator 2.
	if gfExp[0] != 1 || gfExp[1] != 2 || gfExp[8] != 0x1D {
		t.Errorf("gfExp anchors wrong: [0]=%d [1]=%d [8]=%d", gfExp[0], gfExp[1], gfExp[8])
	}
	if gfLog[1] != 0 || gfLog[2] != 1 {
		t.Errorf("gfLog anchors wrong: [1]=%d [2]=%d", gfLog[1], gfLog[2])
	}
	// Round-trip: exp(log(x)) == x for all non-zero x.
	for x := 1; x < 256; x++ {
		if gfExp[gfLog[byte(x)]] != byte(x) {
			t.Fatalf("exp(log(%d)) != %d", x, x)
		}
	}
}

func TestGFMul(t *testing.T) {
	cases := []struct{ a, b, want byte }{
		{0, 5, 0}, {5, 0, 0}, {1, 42, 42}, {2, 2, 4}, {2, 128, 0x1D},
	}
	for _, c := range cases {
		if got := gfMul(c.a, c.b); got != c.want {
			t.Errorf("gfMul(%d,%d) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestRSGenerator(t *testing.T) {
	g := rsGenerator(10)
	if len(g) != 11 {
		t.Fatalf("len(generator) = %d, want 11", len(g))
	}
	if g[0] != 1 {
		t.Errorf("leading coeff = %d, want 1", g[0])
	}
}

func TestECCodewordsDivisibility(t *testing.T) {
	// Defining RS property: (data << n) with the EC codewords appended is
	// exactly divisible by the generator polynomial, i.e. dividing the full
	// codeword stream by the generator leaves a zero remainder.
	data := []byte{32, 91, 11, 120, 209, 114, 220, 77, 67, 64, 236, 17, 236}
	n := 10
	ec := ecCodewords(data, n)
	if len(ec) != n {
		t.Fatalf("len(ec) = %d, want %d", len(ec), n)
	}
	full := append(append([]byte{}, data...), ec...)
	if rem := ecCodewords(full, n); !allZero(rem) {
		t.Errorf("codeword stream not divisible by generator: remainder %v", rem)
	}
}

func allZero(b []byte) bool {
	for _, x := range b {
		if x != 0 {
			return false
		}
	}
	return true
}
