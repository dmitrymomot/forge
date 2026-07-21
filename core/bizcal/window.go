package bizcal

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Window is a time-of-day span expressed as minutes since midnight, with
// 0 <= start < end <= 1440. Fields are unexported so the invariant can only
// be established through NewWindow, ParseWindow, ParseWindows, or MustWindows.
// The zero value has start == end == 0 and is empty/invalid for scheduling;
// it exists only as Go's default zero value, never as a usable window.
type Window struct {
	start, end int
}

// NewWindow constructs a Window from minutes-since-midnight bounds. It
// returns ErrInvalidWindow if start is negative, end exceeds 1440
// (24:00), or start is not strictly less than end.
func NewWindow(startMin, endMin int) (Window, error) {
	if startMin < 0 || endMin > 1440 || startMin >= endMin {
		return Window{}, fmt.Errorf("%w: start=%d end=%d minutes", ErrInvalidWindow, startMin, endMin)
	}
	return Window{start: startMin, end: endMin}, nil
}

// ParseWindow parses a "HH:MM-HH:MM" span, e.g. "09:00-17:30". A single
// digit hour is accepted ("9:00-17:00" is valid). "24:00" is accepted only
// as an end-of-day boundary (minute must be 00). Returns ErrInvalidWindow,
// wrapping detail, for any malformed or out-of-range input.
func ParseWindow(s string) (Window, error) {
	parts := strings.Split(s, "-")
	if len(parts) != 2 {
		return Window{}, fmt.Errorf("%w: %q: expected HH:MM-HH:MM", ErrInvalidWindow, s)
	}

	startMin, ok := parseClock(parts[0])
	if !ok {
		return Window{}, fmt.Errorf("%w: %q: invalid start time", ErrInvalidWindow, s)
	}
	endMin, ok := parseClock(parts[1])
	if !ok {
		return Window{}, fmt.Errorf("%w: %q: invalid end time", ErrInvalidWindow, s)
	}

	w, err := NewWindow(startMin, endMin)
	if err != nil {
		return Window{}, fmt.Errorf("%w: %q", ErrInvalidWindow, s)
	}
	return w, nil
}

// ParseWindows parses each spec via ParseWindow, returning on the first
// error encountered.
func ParseWindows(specs ...string) ([]Window, error) {
	ws := make([]Window, 0, len(specs))
	for _, s := range specs {
		w, err := ParseWindow(s)
		if err != nil {
			return nil, err
		}
		ws = append(ws, w)
	}
	return ws, nil
}

// MustWindows is like ParseWindows but panics on error. Intended for
// literals known to be valid at compile time.
func MustWindows(specs ...string) []Window {
	ws, err := ParseWindows(specs...)
	if err != nil {
		panic(err)
	}
	return ws
}

// Start returns w's start time as (hour, minute).
func (w Window) Start() (hour, min int) {
	return w.start / 60, w.start % 60
}

// End returns w's end time as (hour, minute).
func (w Window) End() (hour, min int) {
	return w.end / 60, w.end % 60
}

// Duration returns the span covered by w.
func (w Window) Duration() time.Duration {
	return time.Duration(w.end-w.start) * time.Minute
}

// String formats w as "09:00-17:30".
func (w Window) String() string {
	sh, sm := w.Start()
	eh, em := w.End()
	return fmt.Sprintf("%02d:%02d-%02d:%02d", sh, sm, eh, em)
}

// parseClock parses a "H:MM" or "HH:MM" clock string into minutes since
// midnight. Hour must be 0-24; minute must be 0-59; hour 24 requires
// minute 00 (the "24:00" end-of-day boundary).
func parseClock(s string) (int, bool) {
	idx := strings.IndexByte(s, ':')
	if idx != 1 && idx != 2 {
		return 0, false
	}
	hourStr, minStr := s[:idx], s[idx+1:]
	if len(minStr) != 2 || !isDigits(hourStr) || !isDigits(minStr) {
		return 0, false
	}

	hour, err := strconv.Atoi(hourStr)
	if err != nil || hour > 24 {
		return 0, false
	}
	minute, err := strconv.Atoi(minStr)
	if err != nil || minute > 59 {
		return 0, false
	}
	if hour == 24 && minute != 0 {
		return 0, false
	}
	return hour*60 + minute, true
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// Interval is a concrete, absolute time span with an exclusive end,
// used for shifts and the result of window-to-instant expansion.
type Interval struct {
	Start, End time.Time
}

// Duration returns the span covered by iv.
func (iv Interval) Duration() time.Duration {
	return iv.End.Sub(iv.Start)
}
