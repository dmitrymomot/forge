package qrcode

var (
	gfExp [256]byte
	gfLog [256]byte
)

func init() {
	x := 1
	for i := range 255 {
		gfExp[i] = byte(x)
		gfLog[byte(x)] = byte(i)
		x <<= 1
		if x&0x100 != 0 {
			x ^= 0x11D // primitive polynomial
		}
	}
	gfExp[255] = gfExp[0] // 2^255 == 1; wrap for convenience
}

// gfMul multiplies two elements of GF(256).
func gfMul(a, b byte) byte {
	if a == 0 || b == 0 {
		return 0
	}
	return gfExp[(int(gfLog[a])+int(gfLog[b]))%255]
}

// rsGenerator returns the degree-n Reed-Solomon generator polynomial,
// product of (x - 2^i) for i in [0, n), coefficients high-order first.
func rsGenerator(n int) []byte {
	g := []byte{1}
	for i := range n {
		// Multiply g(x) by (x - 2^i)  ==  (x + 2^i) in GF(256).
		next := make([]byte, len(g)+1)
		root := gfExp[i]
		for j, c := range g {
			next[j] ^= c                // c * x
			next[j+1] ^= gfMul(c, root) // c * root
		}
		g = next
	}
	return g
}

// ecCodewords returns n Reed-Solomon error-correction codewords for data:
// the remainder of data*x^n divided by the degree-n generator polynomial.
func ecCodewords(data []byte, n int) []byte {
	gen := rsGenerator(n)
	rem := make([]byte, len(data)+n)
	copy(rem, data)
	for i := range data {
		coef := rem[i]
		if coef == 0 {
			continue
		}
		for j, gc := range gen {
			rem[i+j] ^= gfMul(gc, coef)
		}
	}
	return rem[len(data):]
}
