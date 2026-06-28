package clock_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/clock"
)

func TestSystem_NowIsCurrent(t *testing.T) {
	before := time.Now()
	got := clock.System().Now()
	after := time.Now()
	assert.False(t, got.Before(before))
	assert.False(t, got.After(after))
}

func TestMock_Now(t *testing.T) {
	base := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	m := clock.NewMock(base)
	assert.Equal(t, base, m.Now())
}

func TestMock_Set(t *testing.T) {
	m := clock.NewMock(time.Unix(0, 0))
	next := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	m.Set(next)
	assert.Equal(t, next, m.Now())
}

func TestMock_Advance(t *testing.T) {
	base := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	m := clock.NewMock(base)
	m.Advance(90 * time.Minute)
	assert.Equal(t, base.Add(90*time.Minute), m.Now())
}

func TestMock_ImplementsClock(t *testing.T) {
	var _ clock.Clock = clock.NewMock(time.Now())
	var _ clock.Clock = clock.System() //nolint:staticcheck // compile-time interface assertion; QF1011 inference suggestion does not apply
}
