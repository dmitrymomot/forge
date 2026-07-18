package mongoqueue_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	mongoqueue "github.com/dmitrymomot/forge/async/queue/mongo"
)

func TestMongoQueue_ValidatesConstruction(t *testing.T) {
	t.Parallel()
	_, err := mongoqueue.New(nil)
	require.Error(t, err, "nil database rejected")
}
