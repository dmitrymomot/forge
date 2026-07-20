package scheduler

import (
	"fmt"
	"time"
)

// Schedule computes fire times. Next returns the first tick strictly after t,
// or the zero time when the schedule never fires again. Implementations MUST
// be deterministic — every fleet instance computes the same tick sequence, and
// that agreement is what makes the Store claim dedupe fires — and are expected
// to honor t's absolute instant regardless of its location.
type Schedule interface {
	Next(t time.Time) time.Time
}

// Every returns a Schedule that fires every d, with ticks aligned to whole
// multiples of d (time.Truncate alignment), so every fleet instance computes
// identical tick times regardless of when it started. Panics on d <= 0:
// schedules are package-level wiring, not runtime data.
func Every(d time.Duration) Schedule {
	if d <= 0 {
		panic(fmt.Sprintf("scheduler: Every(%v): interval must be > 0", d))
	}
	return intervalSchedule{d: d}
}

type intervalSchedule struct {
	d time.Duration
}

// Next implements Schedule.
func (s intervalSchedule) Next(t time.Time) time.Time {
	return t.Truncate(s.d).Add(s.d)
}
