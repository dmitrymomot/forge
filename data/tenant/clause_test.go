package tenant_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/data/tenant"
)

func TestScopeClause(t *testing.T) {
	t.Parallel()

	ctx := tenant.NewContext(context.Background(), "acme")

	t.Run("dollar placeholder", func(t *testing.T) {
		t.Parallel()
		c, err := tenant.ScopeClause(ctx, "tenant_id", "$2")
		require.NoError(t, err)
		assert.Equal(t, "tenant_id = $2", c.SQL)
		assert.Equal(t, "acme", c.Arg)
	})

	t.Run("question placeholder", func(t *testing.T) {
		t.Parallel()
		c, err := tenant.ScopeClause(ctx, "tenant_id", "?")
		require.NoError(t, err)
		assert.Equal(t, "tenant_id = ?", c.SQL)
		assert.Equal(t, "acme", c.Arg)
	})

	t.Run("qualified column", func(t *testing.T) {
		t.Parallel()
		c, err := tenant.ScopeClause(ctx, "orders.tenant_id", "$1")
		require.NoError(t, err)
		assert.Equal(t, "orders.tenant_id = $1", c.SQL)
	})

	t.Run("fails closed without tenant", func(t *testing.T) {
		t.Parallel()
		_, err := tenant.ScopeClause(context.Background(), "tenant_id", "$1")
		require.ErrorIs(t, err, tenant.ErrNoTenant)
	})

	t.Run("invalid columns", func(t *testing.T) {
		t.Parallel()
		for _, col := range []string{
			"", ".", "1col", "tenant-id", "tenant id", "a.b.c", "a.", ".b", "a..b",
			"tenant_id;DROP TABLE users", `tenant_id"`, "tenant_id = $1 OR 1=1 --",
		} {
			_, err := tenant.ScopeClause(ctx, col, "$1")
			assert.ErrorIs(t, err, tenant.ErrInvalidColumn, "column %q", col)
		}
	})

	t.Run("invalid placeholders", func(t *testing.T) {
		t.Parallel()
		for _, ph := range []string{
			"", "$", "$0", "$01", "$x", "$1 OR 1=1", "??", "?1", ":id", "%s", "$-1",
		} {
			_, err := tenant.ScopeClause(ctx, "tenant_id", ph)
			assert.ErrorIs(t, err, tenant.ErrInvalidPlaceholder, "placeholder %q", ph)
		}
	})

	t.Run("valid identifier shapes", func(t *testing.T) {
		t.Parallel()
		for _, col := range []string{"t", "_t", "T1", "a_b_c", "schema1.col2"} {
			_, err := tenant.ScopeClause(ctx, col, "$10")
			assert.NoError(t, err, "column %q", col)
		}
	})

	t.Run("misuse rejected even without tenant", func(t *testing.T) {
		t.Parallel()
		_, err := tenant.ScopeClause(context.Background(), "bad column", "$1")
		require.ErrorIs(t, err, tenant.ErrInvalidColumn)
	})
}
