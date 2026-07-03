package enum_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/enum"
)

type Status string

const (
	StatusActive Status = "active"
	StatusPaused Status = "paused"
)

var Statuses = enum.New(StatusActive, StatusPaused, StatusActive) // dup ignored

func TestValues_DeclaredOrderAndDedup(t *testing.T) {
	assert.Equal(t, []Status{StatusActive, StatusPaused}, Statuses.Values())
}

func TestValues_Valid(t *testing.T) {
	assert.True(t, Statuses.Valid(StatusActive))
	assert.False(t, Statuses.Valid(Status("deleted")))
}

func TestValues_Parse(t *testing.T) {
	v, err := Statuses.Parse("paused")
	require.NoError(t, err)
	assert.Equal(t, StatusPaused, v)

	_, err = Statuses.Parse("nope")
	require.Error(t, err)
	assert.True(t, errors.Is(err, enum.ErrInvalidValue))
}

func TestValues_ReturnedSliceIsCopy(t *testing.T) {
	vs := Statuses.Values()
	vs[0] = "mutated"
	assert.Equal(t, StatusActive, Statuses.Values()[0], "Values() returns an independent copy")
}
