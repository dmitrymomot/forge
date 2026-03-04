package sqlitedb

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestShutdown_Success(t *testing.T) {
	t.Parallel()

	db, err := Open(context.Background(), Config{Path: ":memory:"})
	require.NoError(t, err)

	shutdown := Shutdown(db)
	err = shutdown(context.Background())
	require.NoError(t, err)

	// Verify db is closed — Ping should fail
	err = db.PingContext(context.Background())
	require.Error(t, err)
}
