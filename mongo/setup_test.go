package mongo_test

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.mongodb.org/mongo-driver/v2/bson"
	mongodriver "go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	forgemongo "github.com/dmitrymomot/forge/mongo"
)

func TestEnsureIndexes_EmptySpecsNoOp(t *testing.T) {
	// Empty (and nil) specs must be a no-op that returns nil without touching the
	// server — so it is safe to call with a nil *mongo.Database in a unit test.
	require.NoError(t, forgemongo.EnsureIndexes(t.Context(), nil, nil))
	require.NoError(t, forgemongo.EnsureIndexes(t.Context(), nil, map[string][]mongodriver.IndexModel{}))
}

func TestEnsureIndexes_Integration(t *testing.T) {
	c := openTestClient(t) // from lifecycle_test.go (skips without FORGE_TEST_MONGO_URI)
	db := c.Database("forge_test")
	t.Cleanup(func() { _ = db.Collection("idx_users").Drop(context.Background()) })

	specs := map[string][]mongodriver.IndexModel{
		"idx_users": {
			{
				Keys:    bson.D{{Key: "email", Value: 1}},
				Options: options.Index().SetUnique(true).SetName("uniq_email"),
			},
			{
				Keys: bson.D{{Key: "created_at", Value: -1}},
			},
		},
	}

	// First run creates the indexes; second run is idempotent (CreateMany by spec).
	require.NoError(t, forgemongo.EnsureIndexes(t.Context(), db, specs))
	require.NoError(t, forgemongo.EnsureIndexes(t.Context(), db, specs), "re-running EnsureIndexes must be idempotent")

	cur, err := db.Collection("idx_users").Indexes().List(t.Context())
	require.NoError(t, err)
	var idx []bson.M
	require.NoError(t, cur.All(t.Context(), &idx))
	// _id_ + uniq_email + created_at index = 3.
	assert.Len(t, idx, 3)
}

func TestSharding_Integration(t *testing.T) {
	// Sharding commands require a mongos (sharded cluster). Gate on a dedicated var.
	uri := os.Getenv("FORGE_TEST_MONGO_SHARDED_URI")
	if uri == "" {
		t.Skip("FORGE_TEST_MONGO_SHARDED_URI not set; sharding needs a mongos")
	}
	cfg := forgemongo.DefaultConfig()
	cfg.URI = uri
	c, err := forgemongo.Open(t.Context(), forgemongo.WithConfig(cfg))
	require.NoError(t, err)
	t.Cleanup(func() { forgemongo.Close(c, nil) })

	require.NoError(t, forgemongo.EnableSharding(t.Context(), c, "forge_test"))
	require.NoError(t, forgemongo.ShardCollection(t.Context(), c, "forge_test.sharded", bson.D{{Key: "_id", Value: "hashed"}}))
}
