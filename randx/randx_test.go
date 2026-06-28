package randx_test

import (
	"encoding/base64"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/randx"
)

func TestBytes_LengthAndUniqueness(t *testing.T) {
	a := randx.Bytes(32)
	b := randx.Bytes(32)
	assert.Len(t, a, 32)
	assert.Len(t, b, 32)
	assert.NotEqual(t, a, b) // astronomically unlikely to collide
}

func TestRead(t *testing.T) {
	p := make([]byte, 16)
	require.NoError(t, randx.Read(p))
	assert.NotEqual(t, make([]byte, 16), p)
}

func TestHex(t *testing.T) {
	s := randx.Hex(8)
	assert.Len(t, s, 16) // 2 hex chars per byte
	_, err := hex.DecodeString(s)
	require.NoError(t, err)
	assert.NotEqual(t, randx.Hex(8), randx.Hex(8))
}

func TestURLSafe(t *testing.T) {
	s := randx.URLSafe(16)
	decoded, err := base64.RawURLEncoding.DecodeString(s)
	require.NoError(t, err)
	assert.Len(t, decoded, 16)
	assert.NotEqual(t, randx.URLSafe(16), randx.URLSafe(16))
}

func TestInt_InRange(t *testing.T) {
	for range 1000 {
		v := randx.Int(10)
		assert.GreaterOrEqual(t, v, 0)
		assert.Less(t, v, 10)
	}
}

func TestInt_PanicsOnNonPositive(t *testing.T) {
	assert.Panics(t, func() { randx.Int(0) })
	assert.Panics(t, func() { randx.Int(-5) })
}
