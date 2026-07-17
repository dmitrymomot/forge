package tenant_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/data/tenant"
)

func TestContextCarrier(t *testing.T) {
	t.Parallel()

	t.Run("round trip", func(t *testing.T) {
		t.Parallel()
		ctx := tenant.NewContext(context.Background(), "acme")
		id, ok := tenant.FromContext(ctx)
		assert.True(t, ok)
		assert.Equal(t, "acme", id)
	})

	t.Run("absent", func(t *testing.T) {
		t.Parallel()
		id, ok := tenant.FromContext(context.Background())
		assert.False(t, ok)
		assert.Empty(t, id)
	})

	t.Run("empty ID reads as absent", func(t *testing.T) {
		t.Parallel()
		ctx := tenant.NewContext(context.Background(), "")
		_, ok := tenant.FromContext(ctx)
		assert.False(t, ok)
	})

	t.Run("inner stamp wins", func(t *testing.T) {
		t.Parallel()
		ctx := tenant.NewContext(tenant.NewContext(context.Background(), "outer"), "inner")
		id, ok := tenant.FromContext(ctx)
		assert.True(t, ok)
		assert.Equal(t, "inner", id)
	})
}

func TestScope(t *testing.T) {
	t.Parallel()

	t.Run("present", func(t *testing.T) {
		t.Parallel()
		id, err := tenant.Scope(tenant.NewContext(context.Background(), "acme"))
		require.NoError(t, err)
		assert.Equal(t, "acme", id)
	})

	t.Run("fails closed when absent", func(t *testing.T) {
		t.Parallel()
		_, err := tenant.Scope(context.Background())
		require.ErrorIs(t, err, tenant.ErrNoTenant)
	})

	t.Run("fails closed on empty ID", func(t *testing.T) {
		t.Parallel()
		_, err := tenant.Scope(tenant.NewContext(context.Background(), ""))
		require.ErrorIs(t, err, tenant.ErrNoTenant)
	})
}
