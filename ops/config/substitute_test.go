package config_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/ops/config"
)

func TestSubstitute(t *testing.T) {
	t.Setenv("HOST", "10.0.0.1")
	t.Setenv("EMPTY", "")

	got, err := config.Substitute("host: ${HOST:0.0.0.0}")
	require.NoError(t, err)
	assert.Equal(t, "host: 10.0.0.1", got)

	got, err = config.Substitute("host: ${MISSING:0.0.0.0}")
	require.NoError(t, err)
	assert.Equal(t, "host: 0.0.0.0", got)

	got, err = config.Substitute("host: ${EMPTY:fallback}") // empty -> default
	require.NoError(t, err)
	assert.Equal(t, "host: fallback", got)

	got, err = config.Substitute("url: ${MISSING:http://x:8080}") // colon in default
	require.NoError(t, err)
	assert.Equal(t, "url: http://x:8080", got)

	got, err = config.Substitute("price: $$5")
	require.NoError(t, err)
	assert.Equal(t, "price: $5", got)

	_, err = config.Substitute("x: ${NOPE}") // unset, no default -> error
	assert.ErrorIs(t, err, config.ErrSubstitute)

	_, err = config.Substitute("x: ${UNTERMINATED")
	assert.ErrorIs(t, err, config.ErrSubstitute)
}
