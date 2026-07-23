package bizcal_test

import (
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/core/bizcal"
)

// TestResolver_ConcurrentDayOps hammers the public day operations from many
// goroutines against a single shared Calendar, exercising the per-year memo
// under the race detector. Dates span three years so multiple year plans
// are built (and re-read) concurrently.
func TestResolver_ConcurrentDayOps(t *testing.T) {
	kyiv, err := time.LoadLocation("Europe/Kyiv")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	cal, err := bizcal.New(kyiv,
		bizcal.WithWorkdays(8*time.Hour, time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday),
		bizcal.WithRule(bizcal.Fixed{Month: time.January, Day: 1}),
		bizcal.WithExceptions(
			bizcal.DayOff(bizcal.MustDate(2026, time.July, 24)),
			bizcal.ShortDay(bizcal.MustDate(2026, time.December, 31), 4*time.Hour),
		),
		bizcal.WithShifts(bizcal.Shift(
			time.Date(2026, time.July, 20, 22, 0, 0, 0, time.UTC),
			time.Date(2026, time.July, 21, 2, 0, 0, 0, time.UTC),
		)),
	)
	if err != nil {
		t.Fatalf("New() = %v", err)
	}

	const goroutines = 32
	const iterations = 300
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := range goroutines {
		go func(seed int64) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(seed))
			for range iterations {
				year := 2025 + rng.Intn(3)
				d := bizcal.MustDate(year, time.January, 1).AddDays(rng.Intn(365))
				cal.IsWorkingDay(d)
				cal.DayDuration(d)
				to := d.AddDays(rng.Intn(60))
				cal.WorkingDays(d, to)
				cal.ScheduledBetween(d, to)
				if _, err := cal.AddWorkingDays(d, rng.Intn(21)-10); err != nil {
					// horizon errors are impossible on this calendar; any
					// error would be a real defect.
					t.Errorf("AddWorkingDays(%s) = %v", d, err)
					return
				}
			}
		}(int64(g) + 1)
	}
	wg.Wait()
}
