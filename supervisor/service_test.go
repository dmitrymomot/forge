package supervisor

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServiceFunc_DelegatesNameAndRun(t *testing.T) {
	called := false
	var gotCtx context.Context
	s := serviceFunc{name: "worker", fn: func(ctx context.Context) error {
		called = true
		gotCtx = ctx
		return nil
	}}

	require.Equal(t, "worker", s.Name())

	type ctxKey struct{}
	ctx := context.WithValue(context.Background(), ctxKey{}, "v")
	require.NoError(t, s.Run(ctx))
	assert.True(t, called, "fn must be invoked by Run")
	assert.Equal(t, "v", gotCtx.Value(ctxKey{}), "Run must pass ctx straight through") //nolint:nilaway // gotCtx is assigned inside the closure called synchronously by s.Run above; nilaway cannot track cross-closure assignment
}

func TestServiceFunc_PropagatesError(t *testing.T) {
	sentinel := errors.New("boom")
	s := serviceFunc{name: "x", fn: func(ctx context.Context) error { return sentinel }}
	require.ErrorIs(t, s.Run(context.Background()), sentinel)
}
