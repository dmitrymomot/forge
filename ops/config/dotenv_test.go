package config_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/ops/config"
)

func TestDotenv_InheritanceAndEnvWins(t *testing.T) {
	// Dotenv writes into the real process env via os.Setenv, which t.Setenv
	// does not restore. Clean up the keys Dotenv introduces so they don't
	// leak into sibling tests in this package.
	t.Cleanup(func() {
		_ = os.Unsetenv("PORT")
		_ = os.Unsetenv("TOKEN")
	})

	// real env wins over files
	t.Setenv("HOST", "realhost")

	require.NoError(t, config.Dotenv("testdata/.env.local", "testdata/.env"))

	assert.Equal(t, "realhost", os.Getenv("HOST")) // real env preserved
	assert.Equal(t, "8080", os.Getenv("PORT"))     // later file (.env) overrides .env.local
	assert.Equal(t, "a:b:c", os.Getenv("TOKEN"))   // quoted value, export prefix stripped
}

func TestDotenv_MissingFile(t *testing.T) {
	assert.ErrorIs(t, config.Dotenv("testdata/nope.env"), config.ErrDotenv)
}
