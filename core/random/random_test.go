package random_test

import (
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/random"
)

func TestBytes_LengthAndUniqueness(t *testing.T) {
	a := random.Bytes(32)
	b := random.Bytes(32)
	assert.Len(t, a, 32)
	assert.Len(t, b, 32)
	assert.NotEqual(t, a, b) // astronomically unlikely to collide
}

func TestRead(t *testing.T) {
	p := make([]byte, 16)
	require.NoError(t, random.Read(p))
	assert.NotEqual(t, make([]byte, 16), p)
}

func TestHex(t *testing.T) {
	s := random.Hex(8)
	assert.Len(t, s, 16) // 2 hex chars per byte
	_, err := hex.DecodeString(s)
	require.NoError(t, err)
	assert.NotEqual(t, random.Hex(8), random.Hex(8))
}

func TestURLSafe(t *testing.T) {
	s := random.URLSafe(16)
	decoded, err := base64.RawURLEncoding.DecodeString(s)
	require.NoError(t, err)
	assert.Len(t, decoded, 16)
	assert.NotEqual(t, random.URLSafe(16), random.URLSafe(16))
}

func TestInt_InRange(t *testing.T) {
	for range 1000 {
		v := random.Int(10)
		assert.GreaterOrEqual(t, v, 0)
		assert.Less(t, v, 10)
	}
}

func TestInt_PanicsOnNonPositive(t *testing.T) {
	assert.Panics(t, func() { random.Int(0) })
	assert.Panics(t, func() { random.Int(-5) })
}

func TestString_LengthAndAlphabet(t *testing.T) {
	for range 100 {
		s := random.String(16, random.Uppercase, random.Digits)
		assert.Len(t, s, 16)
		for _, c := range s {
			assert.True(t, strings.ContainsRune(random.Uppercase+random.Digits, c), "unexpected char %q", c)
		}
	}
}

func TestString_DefaultIsAlphanumeric(t *testing.T) {
	s := random.String(32)
	assert.Len(t, s, 32)
	for _, c := range s {
		assert.True(t, strings.ContainsRune(random.Alphanumeric, c))
	}
}

func TestString_Zero(t *testing.T) {
	assert.Equal(t, "", random.String(0))
}

func TestString_DedupesOverlap(t *testing.T) {
	// Alphanumeric already contains Digits; passing both must not bias digits.
	const n = 20000
	s := random.String(n, random.Alphanumeric, random.Digits)
	digits := 0
	for _, c := range s {
		if c >= '0' && c <= '9' {
			digits++
		}
	}
	frac := float64(digits) / float64(n)
	// 10 of 62 distinct chars are digits => ~0.161. Un-deduped (2x) would be ~0.278.
	assert.InDelta(t, 10.0/62.0, frac, 0.03, "digit fraction suggests charset not de-duplicated")
}

func TestString_PanicsOnEmptyCharsetOrNegative(t *testing.T) {
	assert.Panics(t, func() { random.String(-1) })
	assert.Panics(t, func() { random.String(4, "") })
}

func TestDigitCode(t *testing.T) {
	for range 100 {
		c := random.DigitCode(6)
		assert.Len(t, c, 6)
		for _, r := range c {
			assert.True(t, r >= '0' && r <= '9', "non-digit %q", r)
		}
	}
}

func TestDigitCode_LeadingZerosPossible(t *testing.T) {
	seen := false
	for range 3000 {
		if random.DigitCode(4)[0] == '0' {
			seen = true
			break
		}
	}
	assert.True(t, seen, "leading zero should be possible")
}

func TestDigitCode_PanicsOnNonPositive(t *testing.T) {
	assert.Panics(t, func() { random.DigitCode(0) })
	assert.Panics(t, func() { random.DigitCode(-1) })
}
