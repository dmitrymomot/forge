package featureflag_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/ops/featureflag"
)

// fakeProvider returns canned flags and optionally errors.
type fakeProvider struct {
	flags featureflag.Flags
	err   error
	calls int
}

func (p *fakeProvider) Flag(_ context.Context, key string) (featureflag.Flag, bool, error) {
	p.calls++
	if p.err != nil {
		return featureflag.Flag{}, false, p.err
	}
	f, ok := p.flags[key]
	return f, ok, nil
}

func TestNewValidation(t *testing.T) {
	t.Parallel()

	t.Run("nil provider", func(t *testing.T) {
		t.Parallel()
		_, err := featureflag.New(featureflag.WithProvider(nil))
		assert.ErrorIs(t, err, featureflag.ErrNilProvider)
	})

	t.Run("empty key in typed option", func(t *testing.T) {
		t.Parallel()
		_, err := featureflag.New(featureflag.WithBool("", true))
		assert.ErrorIs(t, err, featureflag.ErrEmptyKey)
	})

	t.Run("empty key in WithFlags", func(t *testing.T) {
		t.Parallel()
		_, err := featureflag.New(featureflag.WithFlags(featureflag.Flags{"": {Enabled: true}}))
		assert.ErrorIs(t, err, featureflag.ErrEmptyKey)
	})

	t.Run("invalid rollout in WithFlags entry", func(t *testing.T) {
		t.Parallel()
		_, err := featureflag.New(featureflag.WithFlags(featureflag.Flags{"f": {Enabled: true, Rollout: 200}}))
		assert.ErrorIs(t, err, featureflag.ErrInvalidRollout)
	})

	t.Run("adjuster on unknown key", func(t *testing.T) {
		t.Parallel()
		_, err := featureflag.New(featureflag.WithRollout("nope", 50))
		assert.ErrorIs(t, err, featureflag.ErrUnknownFlag)
	})

	t.Run("adjuster invalid rollout", func(t *testing.T) {
		t.Parallel()
		_, err := featureflag.New(
			featureflag.WithBool("f", true),
			featureflag.WithRollout("f", 101),
		)
		assert.ErrorIs(t, err, featureflag.ErrInvalidRollout)
	})

	t.Run("no options is a valid empty client", func(t *testing.T) {
		t.Parallel()
		c, err := featureflag.New()
		require.NoError(t, err)
		assert.False(t, c.Bool(t.Context(), "anything", false))
	})
}

func TestProviderPrecedence(t *testing.T) {
	t.Parallel()

	t.Run("first hit wins across providers", func(t *testing.T) {
		t.Parallel()
		first := &fakeProvider{flags: featureflag.Flags{"f": {Value: "first", Enabled: true, Rollout: 100}}}
		second := &fakeProvider{flags: featureflag.Flags{"f": {Value: "second", Enabled: true, Rollout: 100}}}
		c, err := featureflag.New(featureflag.WithProvider(first), featureflag.WithProvider(second))
		require.NoError(t, err)
		assert.Equal(t, "first", c.String(t.Context(), "f", ""))
	})

	t.Run("static set sits at position of first static option", func(t *testing.T) {
		t.Parallel()
		pg := &fakeProvider{flags: featureflag.Flags{"f": {Value: "db", Enabled: true, Rollout: 100}}}
		// provider first, static second → provider wins
		c, err := featureflag.New(
			featureflag.WithProvider(pg),
			featureflag.WithString("f", "static"),
		)
		require.NoError(t, err)
		assert.Equal(t, "db", c.String(t.Context(), "f", ""))

		// static first, provider second → static wins
		c2, err := featureflag.New(
			featureflag.WithString("f", "static"),
			featureflag.WithProvider(pg),
		)
		require.NoError(t, err)
		assert.Equal(t, "static", c2.String(t.Context(), "f", ""))
	})

	t.Run("later static option overrides earlier same key", func(t *testing.T) {
		t.Parallel()
		c, err := featureflag.New(
			featureflag.WithFlags(featureflag.Flags{"f": {Value: "config", Enabled: true, Rollout: 100}}),
			featureflag.WithString("f", "code"),
		)
		require.NoError(t, err)
		assert.Equal(t, "code", c.String(t.Context(), "f", ""))
	})

	t.Run("provider error falls through to next provider", func(t *testing.T) {
		t.Parallel()
		broken := &fakeProvider{err: assert.AnError}
		c, err := featureflag.New(
			featureflag.WithProvider(broken),
			featureflag.WithBool("f", true),
		)
		require.NoError(t, err)
		assert.True(t, c.Bool(t.Context(), "f", false))
	})
}

func TestTypedSourceOptions(t *testing.T) {
	t.Parallel()
	c, err := featureflag.New(
		featureflag.WithBool("b", true),
		featureflag.WithString("s", "hello"),
		featureflag.WithInt("i", 42),
		featureflag.WithFloat64("f", 1.5),
		featureflag.WithDuration("d", 5*time.Second),
		featureflag.WithRollout("b", 100),
		featureflag.WithAllow("b", "role:staff"),
		featureflag.WithDeny("b", "cus_bad"),
	)
	require.NoError(t, err)
	assert.True(t, c.Bool(t.Context(), "b", false))
	assert.Equal(t, "hello", c.String(t.Context(), "s", ""))
	assert.Equal(t, 42, c.Int(t.Context(), "i", 0))
	assert.InDelta(t, 1.5, c.Float64(t.Context(), "f", 0), 1e-9)
	assert.Equal(t, 5*time.Second, c.Duration(t.Context(), "d", 0))
}

func TestClientAll(t *testing.T) {
	t.Parallel()

	t.Run("merges listers with chain precedence", func(t *testing.T) {
		t.Parallel()
		mem := featureflag.NewMemory(featureflag.Flags{
			"shared":   {Value: "mem", Enabled: true, Rollout: 100},
			"only_mem": {Value: "m", Enabled: true, Rollout: 100},
		})
		c, err := featureflag.New(
			featureflag.WithProvider(mem),
			featureflag.WithString("shared", "static"),
			featureflag.WithString("only_static", "s"),
		)
		require.NoError(t, err)
		all, err := c.All(t.Context())
		require.NoError(t, err)
		assert.Equal(t, "mem", all["shared"].Value, "earlier provider wins")
		assert.Equal(t, "m", all["only_mem"].Value)
		assert.Equal(t, "s", all["only_static"].Value)
	})

	t.Run("non-lister providers are skipped", func(t *testing.T) {
		t.Parallel()
		plain := &fakeProvider{flags: featureflag.Flags{"invisible": {Value: "x", Enabled: true, Rollout: 100}}}
		c, err := featureflag.New(featureflag.WithProvider(plain), featureflag.WithBool("visible", true))
		require.NoError(t, err)
		all, err := c.All(t.Context())
		require.NoError(t, err)
		assert.NotContains(t, all, "invisible")
		assert.Contains(t, all, "visible")
	})
}
