package id_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/id"
)

func TestPrefix_RoundTripDefault(t *testing.T) {
	p := id.NewPrefix("user")
	s := p.New()
	assert.True(t, strings.HasPrefix(s, "user_"))
	body, err := p.Parse(s)
	require.NoError(t, err)
	_, err = id.ParseShort(body)
	require.NoError(t, err, "default body must decode as Short")
	assert.True(t, p.Is(s))
	assert.Equal(t, "user", p.Prefix())
}

func TestPrefix_CustomGenerator(t *testing.T) {
	p := id.NewPrefix("tok", id.WithGenerator(func() string { return id.NewULID().String() }))
	s := p.New()
	assert.True(t, strings.HasPrefix(s, "tok_"))
	body, err := p.Parse(s)
	require.NoError(t, err)
	_, err = id.ParseULID(body)
	require.NoError(t, err)
}

func TestPrefix_WrongPrefix(t *testing.T) {
	p := id.NewPrefix("user")
	_, err := p.Parse("org_" + id.NewShort().String())
	assert.ErrorIs(t, err, id.ErrWrongPrefix)
	assert.False(t, p.Is("org_abc"))
}

func TestPrefix_EmptyBody(t *testing.T) {
	p := id.NewPrefix("user")
	_, err := p.Parse("user_")
	assert.ErrorIs(t, err, id.ErrMalformed)
}

func TestPrefix_NoSeparator(t *testing.T) {
	p := id.NewPrefix("user")
	_, err := p.Parse("userabc")
	assert.ErrorIs(t, err, id.ErrWrongPrefix)
}

func TestPrefixed_StaticAliases(t *testing.T) {
	s := id.NewPrefixed("acct")
	assert.True(t, strings.HasPrefix(s, "acct_"))
	body, err := id.ParsePrefixed("acct", s)
	require.NoError(t, err)
	assert.NotEmpty(t, body)
	assert.True(t, id.IsPrefixed("acct", s))
	assert.False(t, id.IsPrefixed("other", s))
}

func TestPrefix_PanicsOnInvalid(t *testing.T) {
	assert.Panics(t, func() { id.NewPrefix("") })
	assert.Panics(t, func() { id.NewPrefix("User") })
	assert.Panics(t, func() { id.NewPrefix("a_b") })
	assert.Panics(t, func() { id.NewPrefix("x", id.WithGenerator(nil)) })
	assert.Panics(t, func() { id.NewPrefixed("") })
}
