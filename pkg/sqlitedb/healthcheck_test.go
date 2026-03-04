package sqlitedb

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHealthcheck_Success(t *testing.T) {
	t.Parallel()

	db, err := Open(context.Background(), Config{Path: ":memory:"})
	require.NoError(t, err)
	defer db.Close()

	check := Healthcheck(db)
	err = check(context.Background())
	require.NoError(t, err)
}

func TestHealthcheck_ClosedDB(t *testing.T) {
	t.Parallel()

	db, err := Open(context.Background(), Config{Path: ":memory:"})
	require.NoError(t, err)
	db.Close()

	check := Healthcheck(db)
	err = check(context.Background())
	require.Error(t, err)
	require.ErrorIs(t, err, ErrHealthcheckFailed)
}
