package ctxkey_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/ctxkey"
)

func TestKey_WithFrom(t *testing.T) {
	k := ctxkey.New[int]("n")
	ctx := k.With(context.Background(), 42)
	v, ok := k.From(ctx)
	require.True(t, ok)
	assert.Equal(t, 42, v)
}

func TestKey_FromMissing(t *testing.T) {
	k := ctxkey.New[int]("n")
	v, ok := k.From(context.Background())
	assert.False(t, ok)
	assert.Zero(t, v)
}

func TestKey_MustFromPresent(t *testing.T) {
	k := ctxkey.New[string]("s")
	ctx := k.With(context.Background(), "hi")
	assert.Equal(t, "hi", k.MustFrom(ctx))
}

func TestKey_MustFromPanics(t *testing.T) {
	k := ctxkey.New[string]("s")
	assert.PanicsWithValue(t, "ctxkey: key s not present in context", func() {
		k.MustFrom(context.Background())
	})
}

func TestKey_NoCollisionSameName(t *testing.T) {
	a := ctxkey.New[int]("dup")
	b := ctxkey.New[int]("dup")
	ctx := a.With(context.Background(), 1)
	_, ok := b.From(ctx) // b must NOT read a's value despite identical name
	assert.False(t, ok)
}

func TestKey_Name(t *testing.T) {
	assert.Equal(t, "user", ctxkey.New[int]("user").Name())
}
