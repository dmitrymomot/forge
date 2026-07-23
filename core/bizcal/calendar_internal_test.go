package bizcal

import (
	"testing"
	"time"
)

// TestNew_ClonesCallerSlices pins the variadic-clone/deep-copy invariant
// directly against Calendar's unexported state: no ops exist yet to observe
// mutation through behavior (that lands in Task 4), so this white-box test
// is the only way to verify New does not alias a caller's slice today.
func TestNew_ClonesCallerSlices(t *testing.T) {
	windows := MustWindows("09:00-17:00")
	start := time.Date(2026, time.July, 21, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, time.July, 21, 17, 0, 0, 0, time.UTC)
	shifts := []Interval{{Start: start, End: end}}

	cal, err := New(time.UTC, WithWeekday(time.Monday, windows...), WithShifts(shifts...))
	if err != nil {
		t.Fatalf("New() = %v, want nil", err)
	}

	var wantWindow Window
	for _, w := range cal.weekly[time.Monday] {
		wantWindow = w
		break
	}
	var wantShift Interval
	for _, iv := range cal.shifts {
		wantShift = iv
		break
	}

	// Mutate the caller's backing arrays after New returns.
	windows[0] = MustWindows("00:00-01:00")[0]
	shifts[0].Start = time.Date(1999, time.January, 1, 0, 0, 0, 0, time.UTC)

	var gotWindow Window
	for _, w := range cal.weekly[time.Monday] {
		gotWindow = w
		break
	}
	if gotWindow != wantWindow {
		t.Fatalf("weekly[Monday][0] = %v after caller mutation, want unchanged %v", gotWindow, wantWindow)
	}
	var gotShift Interval
	for _, iv := range cal.shifts {
		gotShift = iv
		break
	}
	if gotShift != wantShift {
		t.Fatalf("shifts[0] = %v after caller mutation, want unchanged %v", gotShift, wantShift)
	}
}

// TestMergeWindows verifies sorting and merging of overlapping/adjacent
// windows, and that the result never aliases the input slice.
func TestMergeWindows(t *testing.T) {
	in := MustWindows("14:00-18:00", "09:00-13:00", "12:30-14:00")
	got := mergeWindows(in)

	// 09:00-13:00 and 12:30-14:00 overlap, and the result touches
	// 14:00-18:00 exactly at the boundary (adjacent), so all three merge
	// into a single window.
	want := MustWindows("09:00-18:00")
	if len(got) != len(want) {
		t.Fatalf("mergeWindows(%v) = %v, want %v", in, got, want)
	}
	i := 0
	for _, g := range got {
		if g != want[i] {
			t.Fatalf("mergeWindows(%v)[%d] = %v, want %v", in, i, g, want[i])
		}
		i++
	}

	in[0] = MustWindows("00:00-01:00")[0]
	var last Window
	for _, g := range got {
		last = g
	}
	if last == in[0] {
		t.Fatalf("mergeWindows result aliases input slice")
	}
}

// TestMergeIntervals verifies sorting and merging of overlapping/adjacent
// intervals in absolute time, and that the result never aliases the input.
func TestMergeIntervals(t *testing.T) {
	day := func(h int) time.Time { return time.Date(2026, time.July, 21, h, 0, 0, 0, time.UTC) }
	in := []Interval{
		{Start: day(14), End: day(18)},
		{Start: day(9), End: day(13)},
		{Start: day(12), End: day(14)},
	}
	got := mergeIntervals(in)
	want := []Interval{
		{Start: day(9), End: day(18)},
	}
	var first Interval
	for _, iv := range got {
		first = iv
		break
	}
	if len(got) != len(want) || first != want[0] {
		t.Fatalf("mergeIntervals(%v) = %v, want %v", in, got, want)
	}

	in[0].Start = day(0)
	if first == in[0] {
		t.Fatalf("mergeIntervals result aliases input slice")
	}
}
