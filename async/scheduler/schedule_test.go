package scheduler_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/async/scheduler"
)

func TestEvery(t *testing.T) {
	t.Parallel()

	t.Run("aligns to interval multiples", func(t *testing.T) {
		t.Parallel()
		sched := scheduler.Every(time.Hour)
		assert.Equal(t, at("2026-07-20T11:00:00Z"), sched.Next(at("2026-07-20T10:23:45Z")).UTC())
	})

	t.Run("boundary is strictly after", func(t *testing.T) {
		t.Parallel()
		sched := scheduler.Every(time.Hour)
		assert.Equal(t, at("2026-07-20T11:00:00Z"), sched.Next(at("2026-07-20T10:00:00Z")).UTC())
	})

	t.Run("deterministic across instances", func(t *testing.T) {
		t.Parallel()
		sched := scheduler.Every(15 * time.Minute)
		// Two instances asking at different moments within one interval agree
		// on the tick.
		a := sched.Next(at("2026-07-20T10:01:00Z"))
		b := sched.Next(at("2026-07-20T10:14:59Z"))
		assert.Equal(t, a, b)
	})

	t.Run("location independent", func(t *testing.T) {
		t.Parallel()
		sched := scheduler.Every(10 * time.Minute)
		loc := time.FixedZone("plus3", 3*3600)
		utc := at("2026-07-20T10:03:00Z")
		assert.True(t, sched.Next(utc).Equal(sched.Next(utc.In(loc))))
	})

	t.Run("panics on non-positive interval", func(t *testing.T) {
		t.Parallel()
		assert.Panics(t, func() { scheduler.Every(0) })
		assert.Panics(t, func() { scheduler.Every(-time.Second) })
	})
}
