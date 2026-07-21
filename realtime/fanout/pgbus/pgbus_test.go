package pgbus_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/ops/supervisor"
	"github.com/dmitrymomot/forge/realtime/fanout"
	"github.com/dmitrymomot/forge/realtime/fanout/pgbus"
)

var (
	_ fanout.Bus         = (*pgbus.Bus)(nil)
	_ supervisor.Service = (*pgbus.Bus)(nil)
)

// testPool builds a pool without connecting: pgxpool dials lazily.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), "postgres://localhost:5432/unused")
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

func TestNew(t *testing.T) {
	t.Parallel()

	t.Run("nil pool", func(t *testing.T) {
		t.Parallel()
		_, err := pgbus.New(nil)
		assert.Error(t, err)
	})

	t.Run("bad channel", func(t *testing.T) {
		t.Parallel()
		_, err := pgbus.New(testPool(t), pgbus.WithChannel(""))
		assert.Error(t, err)
		_, err = pgbus.New(testPool(t), pgbus.WithChannel(strings.Repeat("x", 64)))
		assert.Error(t, err)
	})

	t.Run("name carries the channel", func(t *testing.T) {
		t.Parallel()
		bus, err := pgbus.New(testPool(t), pgbus.WithChannel("app_events"))
		require.NoError(t, err)
		assert.Equal(t, "fanout.pgbus:app_events", bus.Name())
	})
}

// The envelope-size check runs before any pool I/O, so no live Postgres is
// needed to exercise it.
func TestPublishPayloadTooLarge(t *testing.T) {
	t.Parallel()

	bus, err := pgbus.New(testPool(t))
	require.NoError(t, err)
	err = bus.Publish(context.Background(), "t", make([]byte, 8000))
	assert.ErrorIs(t, err, pgbus.ErrPayloadTooLarge)
}
