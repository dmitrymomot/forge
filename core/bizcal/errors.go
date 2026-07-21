// Package bizcal provides stdlib-only business-calendar arithmetic.
package bizcal

import "errors"

// Sentinel errors for the bizcal package, all errors.Is-matchable.
var (
	// ErrInvalidDate is returned when a civil date does not exist on the calendar (e.g. 2026-02-30, month 13).
	ErrInvalidDate = errors.New("bizcal: invalid date")

	// ErrInvalidWindow is returned when a Window's bounds are malformed, inverted, or out of range.
	ErrInvalidWindow = errors.New("bizcal: invalid window")

	// ErrInvalidWeekday is returned when a time.Weekday value falls outside Sunday through Saturday.
	ErrInvalidWeekday = errors.New("bizcal: invalid weekday")

	// ErrInvalidRule is returned when a holiday Rule is constructed with invalid parameters.
	ErrInvalidRule = errors.New("bizcal: invalid rule")

	// ErrInvalidShift is returned when a shift Interval has an end at or before its start.
	ErrInvalidShift = errors.New("bizcal: invalid shift")

	// ErrInvalidCapacity is returned when a scheduled capacity duration is negative.
	ErrInvalidCapacity = errors.New("bizcal: invalid capacity")

	// ErrNilLocation is returned when a Calendar is constructed with a nil *time.Location.
	ErrNilLocation = errors.New("bizcal: nil location")

	// ErrNeverOpen is returned when a Calendar is statically closed on every day (no base, no shifts).
	ErrNeverOpen = errors.New("bizcal: calendar never open")

	// ErrHorizonExceeded is returned when a search for an open instant or working day exceeds the configured horizon.
	ErrHorizonExceeded = errors.New("bizcal: search horizon exceeded")
)
