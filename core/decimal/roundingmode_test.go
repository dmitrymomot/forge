package decimal_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/decimal"
)

func TestRoundingModeText(t *testing.T) {
	names := map[decimal.RoundingMode]string{
		decimal.HalfEven: "half_even",
		decimal.HalfUp:   "half_up",
		decimal.HalfDown: "half_down",
		decimal.Up:       "up",
		decimal.Down:     "down",
		decimal.Ceil:     "ceil",
		decimal.Floor:    "floor",
	}
	for mode, name := range names {
		assert.Equal(t, name, mode.String())

		text, err := mode.MarshalText()
		require.NoError(t, err)
		assert.Equal(t, name, string(text))

		var back decimal.RoundingMode
		require.NoError(t, back.UnmarshalText([]byte(name)))
		assert.Equal(t, mode, back)
	}

	t.Run("invalid values", func(t *testing.T) {
		bad := decimal.RoundingMode(99)
		assert.Equal(t, "invalid", bad.String())
		_, err := bad.MarshalText()
		assert.ErrorIs(t, err, decimal.ErrSyntax)
		_, err = decimal.RoundingMode(-1).MarshalText()
		assert.ErrorIs(t, err, decimal.ErrSyntax)

		var m decimal.RoundingMode
		assert.ErrorIs(t, m.UnmarshalText([]byte("nearest")), decimal.ErrSyntax)
		assert.ErrorIs(t, m.UnmarshalText([]byte("")), decimal.ErrSyntax)
	})

	t.Run("json round trip", func(t *testing.T) {
		type wrapper struct {
			Mode decimal.RoundingMode `json:"mode"`
		}
		raw, err := json.Marshal(wrapper{Mode: decimal.Ceil})
		require.NoError(t, err)
		assert.JSONEq(t, `{"mode":"ceil"}`, string(raw))

		var w wrapper
		require.NoError(t, json.Unmarshal([]byte(`{"mode":"floor"}`), &w))
		assert.Equal(t, decimal.Floor, w.Mode)
	})
}
