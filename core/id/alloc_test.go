package id_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/core/id"
)

// Generation allocates at most once, and String exactly one string. These are
// the allocation invariants the package promises. Generation is 0 allocs on
// some platforms (e.g. darwin/arm64) but 1 on others (e.g. linux/amd64), because
// crypto/rand.Read's escape analysis is platform-dependent and can force the
// stack buffer to the heap; the <=1 bound guards against real regressions (a
// pointer type, random.Bytes, fmt, etc. would push it far higher) while staying
// portable. If this ever exceeds 1, tighten the implementation, not the bound.

func TestAlloc_Generation(t *testing.T) {
	assert.LessOrEqual(t, testing.AllocsPerRun(1000, func() { _ = id.NewUUID() }), 1.0, "NewUUID")
	assert.LessOrEqual(t, testing.AllocsPerRun(1000, func() { _ = id.NewULID() }), 1.0, "NewULID")
	assert.LessOrEqual(t, testing.AllocsPerRun(1000, func() { _ = id.NewShort() }), 1.0, "NewShort")
}

func TestAlloc_String(t *testing.T) {
	u := id.NewUUID()
	l := id.NewULID()
	s := id.NewShort()
	assert.LessOrEqual(t, testing.AllocsPerRun(1000, func() { _ = u.String() }), 1.0, "UUID.String")
	assert.LessOrEqual(t, testing.AllocsPerRun(1000, func() { _ = l.String() }), 1.0, "ULID.String")
	assert.LessOrEqual(t, testing.AllocsPerRun(1000, func() { _ = s.String() }), 1.0, "Short.String")
}
