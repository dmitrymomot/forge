// Package bizcal provides stdlib-only, data-free business-calendar
// arithmetic. Consumer-declared schedules (per-weekday clock windows,
// day-granular workdays, rostered shifts, or always-open) plus holiday
// rules and per-date exceptions compose into an immutable, goroutine-safe
// Calendar answering instant questions (open? next open? add business
// duration; business time between two instants) and day questions
// (working day? count; add working days; scheduled hours) — DST-correct
// throughout. No embedded country/holiday tables: holiday data is always
// consumer-supplied.
//
// Build one Calendar per tenant or per policy and cache the instance —
// New is the only construction cost, a Calendar never mutates after it
// returns, and every method is safe to call from multiple goroutines at
// once. Rebuild (and swap the cached pointer) only when the underlying
// policy changes; a version-keyed cache never needs invalidation.
//
// # HR: workdays calendar and salary math
//
// The workdays model (WithWorkdays) matches an HR schedule row exactly:
// a set of weekdays, each open the whole civil day with a fixed capacity,
// no invented clock windows. Combine it with a holiday Rule and per-date
// Exceptions for the employee's short days and days off:
//
//	loc, _ := time.LoadLocation("Europe/Kyiv")
//	cal, err := bizcal.New(loc,
//		bizcal.WithWorkdays(8*time.Hour, time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday),
//		bizcal.WithRule(bizcal.Fixed{Month: time.January, Day: 1}),
//		bizcal.WithExceptions(bizcal.ShortDay(bizcal.MustDate(2026, time.December, 31), 4*time.Hour)),
//	)
//
// Salary formulas fall out of the day and instant ops: expected hours for
// a pay period is ScheduledBetween(periodStart, periodEnd); payable
// in-schedule time for one shift is Between(clockIn, clockOut); undertime
// is DayDuration(day) minus worked; overtime is worked minus
// DayDuration(day). Leave math uses AddWorkingDays to land n working days
// after a request date, skipping weekends and holidays automatically.
//
// Between on a workdays calendar counts whole-day-open wall-clock span,
// not clipped to any nominal shift: Between(Tue 09:12, Tue 16:05) returns
// 6h53m regardless of what the employee's expected hours are, because the
// whole day is open. Scheduled capacity for that day stays the fixed
// perDay value (8h above) no matter how long the employee actually
// clocked in or out, and no matter what DST does that day — a workdays
// day's capacity never changes.
//
// # SLA: windows or always-open with rostered shifts
//
// The windows model (WithWeekday) and WithAlwaysOpen give clock-accurate
// SLA arithmetic: real interval durations, not fixed per-day capacity.
// Add a support ticket's opened instant plus its response budget to get
// the deadline:
//
//	sla, err := bizcal.New(loc,
//		bizcal.WithWeekday(time.Monday, bizcal.MustWindows("09:00-18:00")...),
//		bizcal.WithWeekday(time.Tuesday, bizcal.MustWindows("09:00-18:00")...),
//	)
//	deadline, err := sla.Add(opened, 8*time.Hour)
//
// A 24/7 desk uses WithAlwaysOpen instead, and an on-call rota layers
// WithShifts on top of either base (or with no base at all) to add
// rostered coverage windows; shift time adds on top of whatever the base
// resolved a day to, so a shift rostered onto a holiday still counts as
// open. DayDuration on a day carrying both a base window and an
// overlapping shift sums the shift's capacity in addition to the base
// window's, even across the overlap — capacity is an additive count of
// scheduled coverage, not a measure of the open-interval union, so a
// double-covered hour is deliberately counted twice.
//
// # DST
//
// Windows-model and always-open capacity is a real absolute duration: a
// spring-forward day loses the skipped hour (23h span) and a fall-back
// day gains the repeated one (25h span), and Between/Add measure the same
// elapsed absolute time, so an SLA spanning a transition is never over-
// or under-counted. Workdays-model capacity is the fixed perDay value
// from WithWorkdays and is unaffected by DST — an 8h expectation stays
// 8h on a transition day.
//
// # Horizon
//
// NextOpen, Add, and AddWorkingDays scan forward or backward from their
// anchor and are bounded by a configured horizon (WithHorizon, default 5
// years of calendar time); exhausting it returns ErrHorizonExceeded. This
// guards against a holiday Rule that closes every day forever. Between
// and WindowsBetween carry no horizon guard: their cost is proportional
// to the day span between the two instants the caller passed in, so
// callers are expected to pass already-bounded ranges (a pay period, a
// ticket's lifetime) rather than open-ended ones.
package bizcal
