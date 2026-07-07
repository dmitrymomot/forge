package featureflag_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/ops/featureflag"
)

func newClient(t *testing.T, opts ...featureflag.Option) *featureflag.Client {
	t.Helper()
	c, err := featureflag.New(opts...)
	require.NoError(t, err)
	return c
}

func TestEvaluationPipeline(t *testing.T) {
	t.Parallel()

	t.Run("disabled flag returns default", func(t *testing.T) {
		t.Parallel()
		c := newClient(t, featureflag.WithFlags(featureflag.Flags{
			"f": {Value: "true", Enabled: false, Rollout: 100},
		}))
		assert.False(t, c.Bool(t.Context(), "f", false))
		assert.True(t, c.Bool(t.Context(), "f", true), "default is returned, not false")
	})

	t.Run("deny wins over allow", func(t *testing.T) {
		t.Parallel()
		c := newClient(t, featureflag.WithFlags(featureflag.Flags{
			"f": {Value: "true", Enabled: true, Rollout: 100,
				Allow: []string{"usr_1"}, Deny: []string{"usr_1"}},
		}))
		ctx := featureflag.WithSubject(t.Context(), "usr_1")
		assert.False(t, c.Bool(ctx, "f", false))
	})

	t.Run("allow bypasses rollout zero", func(t *testing.T) {
		t.Parallel()
		c := newClient(t, featureflag.WithFlags(featureflag.Flags{
			"f": {Value: "true", Enabled: true, Rollout: 0, Allow: []string{"usr_1"}},
		}))
		assert.True(t, c.Bool(featureflag.WithSubject(t.Context(), "usr_1"), "f", false))
		assert.False(t, c.Bool(featureflag.WithSubject(t.Context(), "usr_2"), "f", false))
	})

	t.Run("identity tokens match allow and deny", func(t *testing.T) {
		t.Parallel()
		c := newClient(t,
			featureflag.WithFlags(featureflag.Flags{
				"vip_only":  {Value: "true", Enabled: true, Rollout: 0, Allow: []string{"segment:vip"}},
				"norm_only": {Value: "true", Enabled: true, Rollout: 100, Deny: []string{"segment:vip"}},
			}),
			featureflag.WithIdentity(func(ctx context.Context) []string {
				if id, _ := fromCtx(ctx); id == "usr_vip" {
					return []string{"segment:vip"}
				}
				return nil
			}),
		)
		vip := featureflag.WithSubject(t.Context(), "usr_vip")
		norm := featureflag.WithSubject(t.Context(), "usr_norm")
		assert.True(t, c.Bool(vip, "vip_only", false))
		assert.False(t, c.Bool(norm, "vip_only", false))
		assert.False(t, c.Bool(vip, "norm_only", false))
		assert.True(t, c.Bool(norm, "norm_only", false))
	})

	t.Run("rollout without subject returns default", func(t *testing.T) {
		t.Parallel()
		c := newClient(t, featureflag.WithFlags(featureflag.Flags{
			"f": {Value: "true", Enabled: true, Rollout: 50},
		}))
		assert.False(t, c.Bool(t.Context(), "f", false), "no subject → deterministic off path")
	})

	t.Run("rollout 100 needs no subject", func(t *testing.T) {
		t.Parallel()
		c := newClient(t, featureflag.WithBool("f", true))
		assert.True(t, c.Bool(t.Context(), "f", false))
	})

	t.Run("empty subject id never matches empty-string tokens", func(t *testing.T) {
		t.Parallel()
		c := newClient(t, featureflag.WithFlags(featureflag.Flags{
			"f": {Value: "true", Enabled: true, Rollout: 0, Allow: []string{"usr_1"}},
		}))
		// no subject in ctx: must not accidentally match anything
		assert.False(t, c.Bool(t.Context(), "f", false))
	})
}

// fromCtx mirrors what an app-side identity resolver does: it reads the
// subject the middleware set. Exercises that WithSubject round-trips.
func fromCtx(ctx context.Context) (string, bool) {
	return featureflag.SubjectFromContext(ctx)
}

func TestRolloutProperties(t *testing.T) {
	t.Parallel()

	flagAt := func(t *testing.T, percent int) *featureflag.Client {
		t.Helper()
		return newClient(t, featureflag.WithFlags(featureflag.Flags{
			"f": {Value: "true", Enabled: true, Rollout: percent},
		}))
	}
	inCohort := func(c *featureflag.Client, id string) bool {
		return c.Bool(featureflag.WithSubject(context.Background(), id), "f", false)
	}

	t.Run("deterministic", func(t *testing.T) {
		t.Parallel()
		c := flagAt(t, 50)
		first := inCohort(c, "usr_42")
		for range 100 {
			assert.Equal(t, first, inCohort(c, "usr_42"))
		}
	})

	t.Run("monotonic ramp keeps earlier cohort", func(t *testing.T) {
		t.Parallel()
		c25, c50 := flagAt(t, 25), flagAt(t, 50)
		for i := range 2000 {
			id := fmt.Sprintf("usr_%d", i)
			if inCohort(c25, id) {
				assert.True(t, inCohort(c50, id), "raising percent must never drop user %s", id)
			}
		}
	})

	t.Run("distribution roughly matches percent", func(t *testing.T) {
		t.Parallel()
		c := flagAt(t, 25)
		hits := 0
		const n = 2000
		for i := range n {
			if inCohort(c, fmt.Sprintf("usr_%d", i)) {
				hits++
			}
		}
		assert.InDelta(t, n*25/100, hits, n*5/100, "25%% ±5pp over %d ids", n)
	})

	t.Run("buckets decorrelated across flags", func(t *testing.T) {
		t.Parallel()
		c := newClient(t, featureflag.WithFlags(featureflag.Flags{
			"a": {Value: "true", Enabled: true, Rollout: 50},
			"b": {Value: "true", Enabled: true, Rollout: 50},
		}))
		same := 0
		const n = 2000
		for i := range n {
			ctx := featureflag.WithSubject(context.Background(), fmt.Sprintf("usr_%d", i))
			if c.Bool(ctx, "a", false) == c.Bool(ctx, "b", false) {
				same++
			}
		}
		// perfectly correlated would be n; independent ≈ n/2
		assert.Less(t, same, n*3/5, "flag buckets must not be correlated")
	})
}

func TestEvaluator(t *testing.T) {
	t.Parallel()

	c := newClient(t, featureflag.WithFlags(featureflag.Flags{
		"rollout50": {Value: "true", Enabled: true, Rollout: 50},
		"vip_only":  {Value: "42", Enabled: true, Rollout: 0, Allow: []string{"segment:vip"}},
	}))

	t.Run("equivalent to ctx carrier", func(t *testing.T) {
		t.Parallel()
		for i := range 200 {
			id := fmt.Sprintf("usr_%d", i)
			viaCtx := c.Bool(featureflag.WithSubject(context.Background(), id), "rollout50", false)
			viaFor := c.For(id).Bool("rollout50", false)
			assert.Equal(t, viaCtx, viaFor, "id %s", id)
		}
	})

	t.Run("explicit tokens substitute for identity resolver", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, 42, c.For("usr_1", "segment:vip").Int("vip_only", 0))
		assert.Equal(t, 0, c.For("usr_1").Int("vip_only", 0))
	})

	t.Run("all typed getters", func(t *testing.T) {
		t.Parallel()
		e := newClient(t,
			featureflag.WithString("s", "x"),
			featureflag.WithFloat64("f", 2.5),
			featureflag.WithDuration("d", time.Minute),
		).For("usr_1")
		assert.Equal(t, "x", e.String("s", ""))
		assert.InDelta(t, 2.5, e.Float64("f", 0), 1e-9)
		assert.Equal(t, time.Minute, e.Duration("d", 0))
	})
}
