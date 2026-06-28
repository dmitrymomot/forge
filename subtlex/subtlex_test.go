package subtlex_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/subtlex"
)

func TestBytesEqual(t *testing.T) {
	assert.True(t, subtlex.BytesEqual([]byte("secret"), []byte("secret")))
	assert.False(t, subtlex.BytesEqual([]byte("secret"), []byte("Secret")))
	assert.False(t, subtlex.BytesEqual([]byte("secret"), []byte("secretX"))) // different length
	assert.True(t, subtlex.BytesEqual(nil, nil))
	assert.True(t, subtlex.BytesEqual([]byte{}, []byte{}))
	assert.False(t, subtlex.BytesEqual([]byte("x"), nil))
}

func TestStringEqual(t *testing.T) {
	assert.True(t, subtlex.StringEqual("token-abc", "token-abc"))
	assert.False(t, subtlex.StringEqual("token-abc", "token-abd"))
	assert.False(t, subtlex.StringEqual("short", "longer-value"))
}
