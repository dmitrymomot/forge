package phone_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/phone"
)

func TestMarshalJSON_RoundTrip(t *testing.T) {
	p, _ := phone.Parse("+14155552671")
	b, err := json.Marshal(p)
	require.NoError(t, err)
	assert.JSONEq(t, `"+14155552671"`, string(b))

	var got phone.Phone
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, "+14155552671", got.E164())
}

func TestMarshalJSON_ZeroIsNull(t *testing.T) {
	var zero phone.Phone
	b, err := json.Marshal(zero)
	require.NoError(t, err)
	assert.Equal(t, "null", string(b))

	var got phone.Phone
	require.NoError(t, json.Unmarshal([]byte("null"), &got))
	assert.True(t, got.IsZero())
}

func TestUnmarshalJSON_Garbage(t *testing.T) {
	var p phone.Phone
	assert.ErrorIs(t, json.Unmarshal([]byte(`"nope"`), &p), phone.ErrMissingCountryCode)
}
