package id_test

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/clock"
	"github.com/dmitrymomot/forge/id"
)

func TestFreeFuncs_ParseRoundTrip(t *testing.T) {
	u, err := id.ParseUUID(id.NewUUID().String())
	require.NoError(t, err)
	assert.False(t, u.IsZero())

	l, err := id.ParseULID(id.NewULID().String())
	require.NoError(t, err)
	assert.False(t, l.IsZero())

	s, err := id.ParseShort(id.NewShort().String())
	require.NoError(t, err)
	assert.False(t, s.IsZero())
}

func TestGenerator_WithClock(t *testing.T) {
	base := time.UnixMilli(1_700_000_000_000).UTC()
	g := id.NewGenerator(id.WithClock(clock.NewMock(base)))
	assert.Equal(t, base.UnixMilli(), g.UUID().Time().UnixMilli())
	assert.Equal(t, base.UnixMilli(), g.ULID().Time().UnixMilli())
	assert.Equal(t, base.UnixMilli(), g.Short().Time().UnixMilli())
}

func TestGenerator_UUIDVersionVariant(t *testing.T) {
	u := id.NewGenerator().UUID()
	assert.Equal(t, byte(0x70), u[6]&0xf0, "version nibble must be 7")
	assert.Equal(t, byte(0x80), u[8]&0xc0, "variant bits must be 0b10")
}

func TestGenerator_MonotonicSameMillisecond(t *testing.T) {
	m := clock.NewMock(time.UnixMilli(1_700_000_000_000).UTC())
	g := id.NewGenerator(id.WithClock(m), id.WithMonotonic())

	const n = 1000
	prev := g.Short()
	for range n {
		cur := g.Short()
		assert.Greater(t, cur.String(), prev.String(), "monotonic short must strictly increase within one ms")
		prev = cur
	}
}

func TestGenerator_MonotonicNoDuplicates(t *testing.T) {
	m := clock.NewMock(time.UnixMilli(1_700_000_000_000).UTC())
	g := id.NewGenerator(id.WithClock(m), id.WithMonotonic())
	seen := make(map[id.ULID]struct{}, 10000)
	for range 10000 {
		u := g.ULID()
		_, dup := seen[u]
		require.False(t, dup)
		seen[u] = struct{}{}
	}
}

func TestGenerator_SortableAcrossTime(t *testing.T) {
	m := clock.NewMock(time.UnixMilli(1_700_000_000_000).UTC())
	g := id.NewGenerator(id.WithClock(m))
	prev := g.ULID()
	for range 100 {
		m.Advance(time.Millisecond)
		cur := g.ULID()
		assert.Greater(t, cur.String(), prev.String())
		prev = cur
	}
}

func TestFreeFuncs_Unique(t *testing.T) {
	seen := make(map[id.ULID]struct{}, 100000)
	for range 100000 {
		u := id.NewULID()
		_, dup := seen[u]
		require.False(t, dup)
		seen[u] = struct{}{}
	}
}

func TestFreeFuncs_ConcurrentUnique(t *testing.T) {
	const workers, per = 8, 10000
	var mu sync.Mutex
	seen := make(map[id.Short]struct{}, workers*per)
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for range per {
				s := id.NewShort()
				mu.Lock()
				seen[s] = struct{}{}
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	// Non-monotonic Short has 32-bit randomness; allow a tiny same-ms collision
	// margin so the test is not flaky, while still proving concurrent safety.
	assert.Greater(t, len(seen), workers*per*99/100)
}
