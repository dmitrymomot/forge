//go:build integration

package mongo_test

import (
	"context"
	"errors"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	forgemongo "github.com/dmitrymomot/forge/data/mongo"
)

// WithTransaction needs a replica set; mongotest provisions a single-node one,
// so openTestDB (shared with the lifecycle suite) already yields a usable db.

func TestWithTransaction_CommitsOnSuccess(t *testing.T) {
	db := openTestDB(t)
	coll := db.Collection("txn_commit")
	t.Cleanup(func() { _ = coll.Drop(context.Background()) })
	require.NoError(t, coll.Drop(t.Context()))

	err := forgemongo.WithTransaction(t.Context(), db, func(ctx context.Context) error {
		_, err := coll.InsertOne(ctx, bson.D{{Key: "k", Value: "v"}})
		return err
	})
	require.NoError(t, err)

	n, err := coll.CountDocuments(t.Context(), bson.D{})
	require.NoError(t, err)
	assert.Equal(t, int64(1), n, "a committed insert must be visible")
}

func TestWithTransaction_AbortsOnError(t *testing.T) {
	db := openTestDB(t)
	coll := db.Collection("txn_abort")
	t.Cleanup(func() { _ = coll.Drop(context.Background()) })
	require.NoError(t, coll.Drop(t.Context()))

	sentinel := errors.New("boom")
	err := forgemongo.WithTransaction(t.Context(), db, func(ctx context.Context) error {
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
