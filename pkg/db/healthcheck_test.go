package db

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestHealthcheck_WrapsPingFailure(t *testing.T) {
	t.Parallel()

	// pgxpool.New is lazy: it does not dial until first use, so we can build a
	// pool against an unreachable address without blocking, then trigger the
	// failure inside the health closure.
	pool, err := pgxpool.New(context.Background(), "postgres://user:pass@127.0.0.1:1/db")
	require.NoError(t, err, "pgxpool.New should not dial eagerly")
	defer pool.Close()

	check := Healthcheck(pool)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err = check(ctx)
	require.Error(t, err, "health check against an unreachable DB must fail")
	require.ErrorIs(t, err, ErrHealthcheckFailed, "failure must be wrapped with ErrHealthcheckFailed")
	// The underlying ping error must be preserved alongside the sentinel.
	require.NotEqual(t, ErrHealthcheckFailed.Error(), err.Error(),
		"underlying ping error must be joined into the returned error")
}
