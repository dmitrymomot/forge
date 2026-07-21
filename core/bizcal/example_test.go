package bizcal_test

import (
	"fmt"
	"time"

	"github.com/dmitrymomot/forge/core/bizcal"
)

// ExampleNew builds an HR workdays calendar (Monday-Friday, 8h/day), then
// derives a pay-period expected total via ScheduledBetween, a single day's
// worked time via Between, and the resulting undertime against that day's
// scheduled capacity.
func ExampleNew() {
	cal, err := bizcal.New(time.UTC,
		bizcal.WithWorkdays(8*time.Hour, time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday),
	)
	if err != nil {
		fmt.Println(err)
		return
	}

	monday := bizcal.MustDate(2026, time.July, 20)
	saturday := bizcal.MustDate(2026, time.July, 25)
	expected := cal.ScheduledBetween(monday, saturday)

	clockIn := time.Date(2026, time.July, 20, 9, 12, 0, 0, time.UTC)
	clockOut := time.Date(2026, time.July, 20, 16, 5, 0, 0, time.UTC)
	worked := cal.Between(clockIn, clockOut)
	undertime := cal.DayDuration(monday) - worked

	fmt.Println(expected)
	fmt.Println(worked)
	fmt.Println(undertime)

	// Output:
	// 40h0m0s
	// 6h53m0s
	// 1h7m0s
}
