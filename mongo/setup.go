package mongo

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"
	mongodriver "go.mongodb.org/mongo-driver/v2/mongo"
)

// EnsureIndexes creates the declared indexes per collection, idempotently. specs
// maps a collection name to its index models; for each entry it calls
// coll.Indexes().CreateMany, which the server treats as create-if-absent by index
// spec, so re-running EnsureIndexes is a safe no-op once the indexes exist. An
// empty or nil specs map is a no-op (db is not touched, so it may be nil). Intended
// to run once at boot, after Open.
func EnsureIndexes(ctx context.Context, db *mongodriver.Database, specs map[string][]mongodriver.IndexModel) error {
	if len(specs) == 0 {
		return nil
	}
	for name, models := range specs {
		if len(models) == 0 {
			continue
		}
		if _, err := db.Collection(name).Indexes().CreateMany(ctx, models); err != nil {
			return fmt.Errorf("mongo: ensure indexes for %q: %w", name, err)
		}
	}
	return nil
}

// EnableSharding enables sharding on a database via the admin "enableSharding"
// command. It requires a sharded (mongos) deployment; on a non-sharded server the
// driver's error is returned verbatim rather than masked.
func EnableSharding(ctx context.Context, c *mongodriver.Client, db string) error {
	cmd := bson.D{{Key: "enableSharding", Value: db}}
	if err := c.Database("admin").RunCommand(ctx, cmd).Err(); err != nil {
		return fmt.Errorf("mongo: enable sharding on %q: %w", db, err)
	}
	return nil
}

// ShardCollection shards a collection via the admin "shardCollection" command.
// namespace is the fully qualified "db.collection"; key is the shard-key spec
// (e.g. bson.D{{Key: "_id", Value: "hashed"}} or a ranged key). It requires a
// sharded (mongos) deployment; on a non-sharded server the driver's error is
// returned verbatim.
func ShardCollection(ctx context.Context, c *mongodriver.Client, namespace string, key bson.D) error {
	cmd := bson.D{
		{Key: "shardCollection", Value: namespace},
		{Key: "key", Value: key},
	}
	if err := c.Database("admin").RunCommand(ctx, cmd).Err(); err != nil {
		return fmt.Errorf("mongo: shard collection %q: %w", namespace, err)
	}
	return nil
}
