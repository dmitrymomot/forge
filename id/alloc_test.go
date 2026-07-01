package id_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/id"
)

// Generation must not touch the heap; String must allocate exactly one string.
// These are the allocation invariants the package promises; if crypto/rand ever
// forces the stack buffer to escape, tighten the implementation rather than the
// bound.

func TestAlloc_Generation(t *testing.T) {
	assert.Zero(t, testing.AllocsPerRun(1000, func() { _ = id.NewUUID() }), "NewUUID")
	assert.Zero(t, testing.AllocsPerRun(1000, func() { _ = id.NewULID() }), "NewULID")
	assert.Zero(t, testing.AllocsPerRun(1000, func() { _ = id.NewShort() }), "NewShort")
}

func TestAlloc_String(t *testing.T) {
	u := id.NewUUID()
	l := id.NewULID()
	s := id.NewShort()
	assert.LessOrEqual(t, testing.AllocsPerRun(1000, func() { _ = u.String() }), 1.0, "UUID.String")
	assert.LessOrEqual(t, testing.AllocsPerRun(1000, func() { _ = l.String() }), 1.0, "ULID.String")
	assert.LessOrEqual(t, testing.AllocsPerRun(1000, func() { _ = s.String() }), 1.0, "Short.String")
}
