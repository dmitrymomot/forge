package loadshed_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/resilience/loadshed"
)

func TestAcquire_AdmitsWhenIdle(t *testing.T) {
	s := loadshed.New(loadshed.WithCriteria(loadshed.Concurrency(4)))
	tk, ok := s.Acquire(context.Background())
	assert.True(t, ok)
	tk.Release()
}

func TestAcquire_ShedsWhenSaturated(t *testing.T) {
	// force pressure high and rand low so the ramp always rejects
	s := loadshed.New(
		loadshed.WithCriteria(loadshed.Concurrency(1)),
		loadshed.WithThreshold(0.0),
		loadshed.WithFloor(0.0),
		loadshed.WithRand(func() float64 { return 0.0 }),
	)
	tk1, ok := s.Acquire(context.Background()) // fills the single slot
	assert.True(t, ok)
	_, ok2 := s.Acquire(context.Background()) // pressure 1.0, reject prob 1.0
	assert.False(t, ok2)
	tk1.Release()
}

func TestPressure_FailsOpenOnPanic(t *testing.T) {
	s := loadshed.New(
		loadshed.WithCriteria(panicCriteria{}),
		loadshed.WithThreshold(0.0),
		loadshed.WithRand(func() float64 { return 0.0 }),
	)
	_, ok := s.Acquire(context.Background())
	assert.True(t, ok) // panic → pressure 0 → admit
}

type panicCriteria struct{}

func (panicCriteria) Pressure() float64 { panic("boom") }
