package mongo_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	mongodriver "go.mongodb.org/mongo-driver/v2/mongo"

	forgemongo "github.com/dmitrymomot/forge/data/mongo"
)

func TestEnsureIndexes_EmptySpecsNoOp(t *testing.T) {
	// Empty (and nil) specs must be a no-op that returns nil without touching the
	// server — so it is safe to call with a nil *mongo.Database in a unit test.
	require.NoError(t, forgemongo.EnsureIndexes(t.Context(), nil, nil))
	require.NoError(t, forgemongo.EnsureIndexes(t.Context(), nil, map[string][]mongodriver.IndexModel{}))
}
