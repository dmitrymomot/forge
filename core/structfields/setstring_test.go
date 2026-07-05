package structfields_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/structfields"
)

func TestSetString_SupportedKinds(t *testing.T) {
	var s struct {
		Name    string
		Count   int
		Small   int32
		Size    uint16
		Ratio   float64
		On      bool
		Timeout time.Duration
		At      time.Time
		Origins []string
	}
	apply := func(name, raw string) {
		require.NoError(t, structfields.Walk(&s, "env", func(f structfields.Field) error {
			if f.Name == name {
				return structfields.SetString(f, raw)
			}
			return nil
		}))
	}
	apply("Name", "forge")
	apply("Count", "42")
	apply("Small", "7")
	apply("Size", "9")
	apply("Ratio", "3.5")
	apply("On", "true")
	apply("Timeout", "1500ms")
	apply("At", "2026-07-05T10:00:00Z")
	apply("Origins", "a,b,c")

	assert.Equal(t, "forge", s.Name)
	assert.Equal(t, 42, s.Count)
	assert.Equal(t, int32(7), s.Small)
	assert.Equal(t, uint16(9), s.Size)
	assert.InDelta(t, 3.5, s.Ratio, 1e-9)
	assert.True(t, s.On)
	assert.Equal(t, 1500*time.Millisecond, s.Timeout)
	assert.Equal(t, 2026, s.At.Year())
	assert.Equal(t, []string{"a", "b", "c"}, s.Origins)
}

func TestSetString_UnsupportedKind(t *testing.T) {
	var s struct{ M map[string]string }
	err := structfields.Walk(&s, "env", func(f structfields.Field) error {
		return structfields.SetString(f, "x")
	})
	assert.ErrorIs(t, err, structfields.ErrUnsupportedKind)
}

func TestSetString_BadSyntax(t *testing.T) {
	var s struct{ Count int }
	err := structfields.Walk(&s, "env", func(f structfields.Field) error {
		return structfields.SetString(f, "not-a-number")
	})
	require.Error(t, err)
}
