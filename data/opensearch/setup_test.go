package opensearch_test

import (
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	forgeos "github.com/dmitrymomot/forge/data/opensearch"
)

func TestSetup_ParseRejectsMalformed(t *testing.T) {
	// A malformed index JSON file must make Apply fail with ErrSetup before any
	// network call. parseSetupFS runs first inside Apply, so passing a nil client is
	// safe — the parse error short-circuits before the client is used.
	fsys := fstest.MapFS{
		"users.index.json": {Data: []byte("{ this is not json ")},
	}
	setup := forgeos.NewSetup(fsys)
	require.NotNil(t, setup)

	err := setup.Apply(t.Context(), nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, forgeos.ErrSetup)
}

func TestSetup_NilFSIsError(t *testing.T) {
	err := forgeos.NewSetup(nil).Apply(t.Context(), nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, forgeos.ErrSetup)
}
