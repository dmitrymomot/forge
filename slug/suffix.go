package slug

import "github.com/dmitrymomot/forge/random"

const (
	// lowerAlphabet is the bias-free [a-z0-9] suffix alphabet (36 runes).
	lowerAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	// mixedAlphabet adds A-Z for the WithLowercase(false) case (62 runes).
	mixedAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	// defaultSuffixLength is used when a reserved-slug hit forces a suffix but no
	// explicit WithSuffix length was set.
	defaultSuffixLength = 6
)

// randomSuffix returns a random string of the given length. Each character is
// drawn from a bias-free [a-z0-9] alphabet (or [a-zA-Z0-9] when lowercase is
// false) using random.Int, which is unbiased — no modulo bias. Returns "" for
// length <= 0.
func randomSuffix(length int, lowercase bool) string {
	if length <= 0 {
		return ""
	}
	alphabet := lowerAlphabet
	if !lowercase {
		alphabet = mixedAlphabet
	}
	runes := []rune(alphabet)
	out := make([]rune, length)
	for i := range out {
		out[i] = runes[random.Int(len(runes))]
	}
	return string(out)
}
