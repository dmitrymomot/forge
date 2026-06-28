package password_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/kdf"
	"github.com/dmitrymomot/forge/password"
)

// Light params keep the memory-hard hash fast in tests.
func lightParams() kdf.Params {
	return kdf.Params{Time: 1, Memory: 8 * 1024, Threads: 1, KeyLen: 32}
}

func TestHashVerify_Argon2id(t *testing.T) {
	enc, err := password.Hash("hunter2", password.WithArgon2Params(lightParams()))
	require.NoError(t, err)
	assert.Contains(t, enc, "$argon2id$")

	ok, _, err := password.Verify("hunter2", enc)
	require.NoError(t, err)
	assert.True(t, ok)

	ok2, needsRehash, err2 := password.Verify("wrong", enc)
	require.NoError(t, err2)
	assert.False(t, ok2)
	assert.False(t, needsRehash)
}

func TestVerify_NeedsRehash(t *testing.T) {
	// Hashed with weaker-than-default params → should request a rehash.
	enc, err := password.Hash("pw", password.WithArgon2Params(lightParams()))
	require.NoError(t, err)
	ok, needsRehash, err := password.Verify("pw", enc)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.True(t, needsRehash)
}

func TestVerify_DefaultParamsNoRehash(t *testing.T) {
	enc, err := password.Hash("pw") // default params
	require.NoError(t, err)
	ok, needsRehash, err := password.Verify("pw", enc)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.False(t, needsRehash)
}

func TestHashVerify_Bcrypt(t *testing.T) {
	enc, err := password.Hash("hunter2",
		password.WithAlgorithm(password.Bcrypt),
		password.WithBcryptCost(4)) // minimum cost for fast tests
	require.NoError(t, err)
	assert.Contains(t, enc, "$2")

	ok, needsRehash, err := password.Verify("hunter2", enc)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.True(t, needsRehash) // bcrypt stored, argon2id is the target → migrate on login

	ok, _, err = password.Verify("wrong", enc)
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestVerify_Malformed(t *testing.T) {
	_, _, err := password.Verify("pw", "not-a-hash")
	require.ErrorIs(t, err, password.ErrInvalidHash)

	_, _, err = password.Verify("pw", "$argon2id$v=19$bad")
	require.ErrorIs(t, err, password.ErrInvalidHash)
}

func TestVerify_UnsupportedArgonVersion(t *testing.T) {
	enc, err := password.Hash("pw", password.WithArgon2Params(lightParams()))
	require.NoError(t, err)
	// Tamper the argon2 version tag to an unsupported value.
	bad := strings.Replace(enc, "$v=19$", "$v=20$", 1)
	require.NotEqual(t, enc, bad) // the replace must have happened
	_, _, err = password.Verify("pw", bad)
	require.ErrorIs(t, err, password.ErrInvalidHash)
}
