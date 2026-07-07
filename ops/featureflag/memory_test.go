package featureflag_test

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/ops/featureflag"
)

func TestMemoryProvider(t *testing.T) {
	t.Parallel()

	t.Run("initial set and lookup", func(t *testing.T) {
		t.Parallel()
		m := featureflag.NewMemory(featureflag.Flags{
			"dark_mode": {Value: "true", Enabled: true, Rollout: 100},
		})
		f, ok, err := m.Flag(t.Context(), "dark_mode")
		require.NoError(t, err)
		require.True(t, ok)
		assert.Equal(t, "true", f.Value)

		_, ok, err = m.Flag(t.Context(), "missing")
		require.NoError(t, err)
		assert.False(t, ok)
	})

	t.Run("nil initial is empty", func(t *testing.T) {
		t.Parallel()
		m := featureflag.NewMemory(nil)
		_, ok, err := m.Flag(t.Context(), "any")
		require.NoError(t, err)
		assert.False(t, ok)
	})

	t.Run("Set validates and is visible", func(t *testing.T) {
		t.Parallel()
		m := featureflag.NewMemory(nil)
		require.ErrorIs(t, m.Set("", featureflag.Flag{Enabled: true}), featureflag.ErrEmptyKey)
		require.ErrorIs(t, m.Set("f", featureflag.Flag{Enabled: true, Rollout: 101}), featureflag.ErrInvalidRollout)

		require.NoError(t, m.Set("maintenance", featureflag.Flag{Value: "true", Enabled: true, Rollout: 100}))
		f, ok, err := m.Flag(t.Context(), "maintenance")
		require.NoError(t, err)
		require.True(t, ok)
		assert.Equal(t, "true", f.Value)
	})

	t.Run("Set clones token slices", func(t *testing.T) {
		t.Parallel()
		m := featureflag.NewMemory(nil)
		allow := []string{"role:staff"}
		require.NoError(t, m.Set("f", featureflag.Flag{Value: "x", Enabled: true, Rollout: 100, Allow: allow}))
		allow[0] = "role:hacked"
		f, _, _ := m.Flag(t.Context(), "f")
		assert.Equal(t, []string{"role:staff"}, f.Allow)
	})

	t.Run("Delete", func(t *testing.T) {
		t.Parallel()
		m := featureflag.NewMemory(featureflag.Flags{"f": {Value: "x", Enabled: true, Rollout: 100}})
		m.Delete("f")
		_, ok, _ := m.Flag(t.Context(), "f")
		assert.False(t, ok)
	})

	t.Run("All returns a copy", func(t *testing.T) {
		t.Parallel()
		m := featureflag.NewMemory(featureflag.Flags{"f": {Value: "x", Enabled: true, Rollout: 100}})
		all, err := m.All(t.Context())
		require.NoError(t, err)
		all["f"] = featureflag.Flag{Value: "mutated"}
		f, _, _ := m.Flag(t.Context(), "f")
		assert.Equal(t, "x", f.Value)
	})

	t.Run("concurrent Set and Flag", func(t *testing.T) {
		t.Parallel()
		m := featureflag.NewMemory(nil)
		var wg sync.WaitGroup
		for i := range 50 {
			wg.Add(2)
			go func() {
				defer wg.Done()
				_ = m.Set("f", featureflag.Flag{Value: "true", Enabled: i%2 == 0, Rollout: 100})
			}()
			go func() {
				defer wg.Done()
				_, _, _ = m.Flag(t.Context(), "f")
			}()
		}
		wg.Wait()
	})
}

// compile-time seam checks
var (
	_ featureflag.Provider = (*featureflag.Memory)(nil)
	_ featureflag.Lister   = (*featureflag.Memory)(nil)
)
