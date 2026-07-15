//go:build integration

package opensearch_test

import (
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/require"

	forgeos "github.com/dmitrymomot/forge/data/opensearch"
	"github.com/dmitrymomot/forge/testkit/opensearchtest"
)

func TestSetup_Integration_Idempotent(t *testing.T) {
	cfg := forgeos.DefaultConfig()
	cfg.Addresses = []string{opensearchtest.Addr(t)}
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
