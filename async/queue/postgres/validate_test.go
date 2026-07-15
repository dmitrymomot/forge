package pgqueue_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	pgqueue "github.com/dmitrymomot/forge/async/queue/postgres"
)

func TestPgQueue_ValidatesConstruction(t *testing.T) {
	t.Parallel()
	_, err := pgqueue.New(nil)
	require.Error(t, err, "nil pool rejected")
}
