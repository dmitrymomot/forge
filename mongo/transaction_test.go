package mongo_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.mongodb.org/mongo-driver/v2/bson"
	mongodriver "go.mongodb.org/mongo-driver/v2/mongo"

	forgemongo "github.com/dmitrymomot/forge/mongo"
)

// WithTransaction needs a replica set / mongos. Gate on a dedicated env var so a
// standalone FORGE_TEST_MONGO_URI does not fail the suite.
func replicaSetURI(t *testing.T) string {
	t.Helper()
	uri := os.Getenv("FORGE_TEST_MONGO_RS_URI")
	if uri == "" {
		t.Skip("FORGE_TEST_MONGO_RS_URI not set; WithTransaction needs a replica set")
	}
	return uri
}

func openRSClient(t *testing.T) *mongodriver.Client {
	t.Helper()
	cfg := forgemongo.DefaultConfig()
	cfg.URI = replicaSetURI(t)
	c, err := forgemongo.Open(t.Context(), forgemongo.WithConfig(cfg))
	require.NoError(t, err)
	t.Cleanup(func() { forgemongo.Close(c, nil) })
	return c
}

func TestWithTransaction_CommitsOnSuccess(t *testing.T) {
	c := openRSClient(t)
	coll := c.Database("forge_test").Collection("txn_commit")
	t.Cleanup(func() { _ = coll.Drop(context.Background()) })
	require.NoError(t, coll.Drop(t.Context()))

	err := forgemongo.WithTransaction(t.Context(), c, func(ctx context.Context) error {
		_, err := coll.InsertOne(ctx, bson.D{{Key: "k", Value: "v"}})
		return err
	})
	require.NoError(t, err)

	n, err := coll.CountDocuments(t.Context(), bson.D{})
	require.NoError(t, err)
	assert.Equal(t, int64(1), n, "a committed insert must be visible")
}

func TestWithTransaction_AbortsOnError(t *testing.T) {
	c := openRSClient(t)
	coll := c.Database("forge_test").Collection("txn_abort")
	t.Cleanup(func() { _ = coll.Drop(context.Background()) })
	require.NoError(t, coll.Drop(t.Context()))

	sentinel := errors.New("boom")
	err := forgemongo.WithTransaction(t.Context(), c, func(ctx context.Context) error {
		if _, e := coll.InsertOne(ctx, bson.D{{Key: "k", Value: "v"}}); e != nil {
			return e
		}
		return sentinel // force an abort after the write
	})
	require.ErrorIs(t, err, sentinel, "fn's error must propagate")

	n, err := coll.CountDocuments(t.Context(), bson.D{})
	require.NoError(t, err)
	assert.Equal(t, int64(0), n, "an aborted insert must not be visible")
}
