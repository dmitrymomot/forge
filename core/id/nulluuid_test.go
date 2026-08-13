package id_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/id"
)

func TestNullUUID_ValueNullAndSet(t *testing.T) {
	v, err := id.NullUUID{}.Value()
	require.NoError(t, err)
	assert.Nil(t, v)

	u := id.NewUUID()
	v, err = id.NullOf(u).Value()
	require.NoError(t, err)
	assert.Equal(t, u.String(), v)
}

func TestNullUUID_ScanNullClearsValid(t *testing.T) {
	n := id.NullOf(id.NewUUID())
	require.NoError(t, n.Scan(nil))
	assert.False(t, n.Valid)
	assert.True(t, n.UUID.IsZero())
}

func TestNullUUID_ScanSources(t *testing.T) {
	u := id.NewUUID()
	tests := []struct {
		name string
		src  any
	}{
		{"canonical string", u.String()},
		{"canonical bytes", []byte(u.String())},
		{"raw 16 bytes", u[:]},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var n id.NullUUID
			require.NoError(t, n.Scan(tt.src))
			assert.True(t, n.Valid)
			assert.Equal(t, u, n.UUID)
		})
	}
}

func TestNullUUID_ScanMalformedStaysInvalid(t *testing.T) {
	n := id.NullOf(id.NewUUID())
	err := n.Scan("not-a-uuid")
	require.Error(t, err)
	assert.ErrorIs(t, err, id.ErrMalformed)
	assert.False(t, n.Valid)
}

func TestNullUUID_JSONRoundTrip(t *testing.T) {
	u := id.NewUUID()
	tests := []struct {
		name string
		in   id.NullUUID
		want string
	}{
		{"null", id.NullUUID{}, `null`},
		{"set", id.NullOf(u), `"` + u.String() + `"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := json.Marshal(tt.in)
			require.NoError(t, err)
			assert.JSONEq(t, tt.want, string(b))

			var got id.NullUUID
			require.NoError(t, json.Unmarshal(b, &got))
			assert.Equal(t, tt.in, got)
		})
	}
}

func TestNullUUID_UnmarshalJSONRejectsMalformed(t *testing.T) {
	var n id.NullUUID
	assert.ErrorIs(t, json.Unmarshal([]byte(`"nope"`), &n), id.ErrMalformed)
	assert.ErrorIs(t, json.Unmarshal([]byte(`42`), &n), id.ErrMalformed)
}

func TestNullUUID_PtrRoundTrip(t *testing.T) {
	assert.Nil(t, id.NullUUID{}.Ptr())
	assert.Equal(t, id.NullUUID{}, id.NullFromPtr(nil))

	u := id.NewUUID()
	assert.Equal(t, u, *id.NullOf(u).Ptr())
	assert.Equal(t, id.NullOf(u), id.NullFromPtr(&u))
}

func TestNullUUID_GetReportsValidity(t *testing.T) {
	_, ok := id.NullUUID{}.Get()
	assert.False(t, ok)

	u := id.NewUUID()
	got, ok := id.NullOf(u).Get()
	assert.True(t, ok)
	assert.Equal(t, u, got)
}
