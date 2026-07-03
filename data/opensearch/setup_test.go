package opensearch_test

import (
	"os"
	"testing"
	"testing/fstest"
	"time"

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

func TestSetup_Integration_Idempotent(t *testing.T) {
	addr := os.Getenv("FORGE_TEST_OPENSEARCH_ADDR")
	if addr == "" {
		t.Skip("set FORGE_TEST_OPENSEARCH_ADDR to run the opensearch integration test")
	}
	cfg := forgeos.DefaultConfig()
	cfg.Addresses = []string{addr}
	cfg.RetryAttempts = 5
	cfg.RetryInterval = 500 * time.Millisecond
	cfg.RequestTimeout = 5 * time.Second

	client, err := forgeos.Open(t.Context(), forgeos.WithConfig(cfg))
	require.NoError(t, err)
	defer forgeos.Close(client, nil)

	fsys := fstest.MapFS{
		"forge_setup_test.index.json": {Data: []byte(`{
			"settings": {"number_of_shards": 1, "number_of_replicas": 0},
			"mappings": {"properties": {"name": {"type": "keyword"}}}
		}`)},
		"forge_setup_test.template.json": {Data: []byte(`{
			"index_patterns": ["forge_setup_logs-*"],
			"template": {"settings": {"number_of_shards": 1}}
		}`)},
	}

	setup := forgeos.NewSetup(fsys, forgeos.WithUpdateMappings(true))

	// First Apply creates the index and upserts the template.
	require.NoError(t, setup.Apply(t.Context(), client))
	// Second Apply is a no-op (index already present; template re-upsert is idempotent).
	require.NoError(t, setup.Apply(t.Context(), client))
}
