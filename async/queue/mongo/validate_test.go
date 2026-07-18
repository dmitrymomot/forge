package mongoqueue_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	mongodriver "go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	mongoqueue "github.com/dmitrymomot/forge/async/queue/mongo"
)

func TestMongoQueue_ValidatesConstruction(t *testing.T) {
	t.Parallel()
	_, err := mongoqueue.New(nil)
	require.Error(t, err, "nil database rejected")

	// Connect performs no I/O in driver v2, so name validation — a pure
	// regexp check inside New — stays in the Docker-free unit tier.
	client, err := mongodriver.Connect(options.Client().ApplyURI("mongodb://127.0.0.1:1"))
	require.NoError(t, err)
	db := client.Database("unit")

	_, err = mongoqueue.New(db, mongoqueue.WithCollection("bad$name"))
	require.Error(t, err, "unsafe collection name rejected")
	_, err = mongoqueue.New(db, mongoqueue.WithCollection(""))
	require.Error(t, err, "empty collection name rejected")

	_, err = mongoqueue.New(db)
	require.NoError(t, err, "default collection name accepted")
}
