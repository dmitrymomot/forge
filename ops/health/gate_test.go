package health_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/ops/health"
)

func TestGate_FlipsReadiness(t *testing.T) {
	gate := health.NewGate()
	require.NoError(t, gate.Check(context.Background())) // up by default

	h := health.Handler(health.WithCheck("accepting", gate.Check))

	rec := do(h)
	assert.Equal(t, http.StatusOK, rec.Code)

	gate.Down()
	assert.ErrorIs(t, gate.Check(context.Background()), health.ErrDraining)

	rec = do(h)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)

	gate.Up()
	rec = do(h)
	assert.Equal(t, http.StatusOK, rec.Code)
}
