package consttime_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/consttime"
)

func TestBytesEqual(t *testing.T) {
	assert.True(t, consttime.BytesEqual([]byte("secret"), []byte("secret")))
	assert.False(t, consttime.BytesEqual([]byte("secret"), []byte("Secret")))
	assert.False(t, consttime.BytesEqual([]byte("secret"), []byte("secretX"))) // different length
	assert.True(t, consttime.BytesEqual(nil, nil))
	assert.True(t, consttime.BytesEqual([]byte{}, []byte{}))
	assert.False(t, consttime.BytesEqual([]byte("x"), nil))
	assert.False(t, consttime.BytesEqual(nil, []byte("x")))      // symmetric to the existing ("x", nil) case
	assert.False(t, consttime.BytesEqual([]byte{}, []byte("x"))) // empty vs non-empty
}

func TestStringEqual(t *testing.T) {
	assert.True(t, consttime.StringEqual("token-abc", "token-abc"))
	assert.False(t, consttime.StringEqual("token-abc", "token-abd"))
	assert.False(t, consttime.StringEqual("short", "longer-value"))
}
