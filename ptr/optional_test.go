package ptr_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/ptr"
)

func TestOptional_SomeNone(t *testing.T) {
	s := ptr.Some(10)
	v, ok := s.Get()
	assert.True(t, ok)
	assert.Equal(t, 10, v)
	assert.True(t, s.IsDefined())
	assert.False(t, s.IsZero())
	assert.Equal(t, 10, s.OrElse(99))

	n := ptr.None[int]()
	_, ok = n.Get()
	assert.False(t, ok)
	assert.False(t, n.IsDefined())
	assert.True(t, n.IsZero())
	assert.Equal(t, 99, n.OrElse(99))
}

func TestOptional_Marshal(t *testing.T) {
	b, err := json.Marshal(ptr.Some("x"))
	require.NoError(t, err)
	assert.JSONEq(t, `"x"`, string(b))

	b, err = json.Marshal(ptr.None[string]())
	require.NoError(t, err)
	assert.Equal(t, "null", string(b))
}

type patch struct {
	Name ptr.Optional[string] `json:"name,omitzero"`
}

func TestOptional_OmitzeroAndAbsentVsNull(t *testing.T) {
	// None omitted entirely on output via omitzero.
	b, err := json.Marshal(patch{Name: ptr.None[string]()})
	require.NoError(t, err)
	assert.JSONEq(t, `{}`, string(b))

	// Some serialized.
	b, err = json.Marshal(patch{Name: ptr.Some("hi")})
	require.NoError(t, err)
	assert.JSONEq(t, `{"name":"hi"}`, string(b))

	// Absent key -> not defined.
	var p patch
	require.NoError(t, json.Unmarshal([]byte(`{}`), &p))
	assert.False(t, p.Name.IsDefined(), "absent key => not defined")

	// Explicit null -> defined, zero value.
	p = patch{}
	require.NoError(t, json.Unmarshal([]byte(`{"name":null}`), &p))
	assert.True(t, p.Name.IsDefined(), "present null => defined")
	v, _ := p.Name.Get()
	assert.Equal(t, "", v)

	// Present value -> defined.
	p = patch{}
	require.NoError(t, json.Unmarshal([]byte(`{"name":"set"}`), &p))
	assert.True(t, p.Name.IsDefined())
	v, _ = p.Name.Get()
	assert.Equal(t, "set", v)
}

func TestOptional_PointerNullVsAbsent(t *testing.T) {
	type patchP struct {
		Bio ptr.Optional[*string] `json:"bio,omitzero"`
	}
	// Explicit null on a pointer T: defined, inner pointer nil (clear the field).
	var p patchP
	require.NoError(t, json.Unmarshal([]byte(`{"bio":null}`), &p))
	assert.True(t, p.Bio.IsDefined())
	inner, _ := p.Bio.Get()
	assert.Nil(t, inner, "explicit null => defined with nil pointer")

	// Absent: not defined (don't touch).
	p = patchP{}
	require.NoError(t, json.Unmarshal([]byte(`{}`), &p))
	assert.False(t, p.Bio.IsDefined())
}
