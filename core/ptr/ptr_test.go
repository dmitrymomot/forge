package ptr_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/core/ptr"
)

func TestFrom(t *testing.T) {
	assert.Equal(t, 7, ptr.From(new(7)))
	assert.Equal(t, 0, ptr.From[int](nil), "nil -> zero value")
}

func TestFromOr(t *testing.T) {
	assert.Equal(t, 7, ptr.FromOr(new(7), 99))
	assert.Equal(t, 99, ptr.FromOr[int](nil, 99))
}

func TestEqual(t *testing.T) {
	assert.True(t, ptr.Equal[int](nil, nil), "both nil equal")
	assert.False(t, ptr.Equal(new(1), nil), "one nil unequal")
	assert.False(t, ptr.Equal[int](nil, new(1)))
	assert.True(t, ptr.Equal(new(5), new(5)))
	assert.False(t, ptr.Equal(new(5), new(6)))
}
