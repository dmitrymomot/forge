package bizcal_test

import (
	"errors"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/core/bizcal"
)

func TestNewDate_Valid(t *testing.T) {
	d, err := bizcal.NewDate(2026, time.July, 21)
	if err != nil {
		t.Fatalf("NewDate: unexpected error: %v", err)
	}
	if d.Year != 2026 || d.Month != time.July || d.Day != 21 {
		t.Fatalf("NewDate: got %+v", d)
	}
}

func TestNewDate_RejectsImpossibleDayOfMonth(t *testing.T) {
	_, err := bizcal.NewDate(2026, time.February, 30)
	if !errors.Is(err, bizcal.ErrInvalidDate) {
		t.Fatalf("NewDate(2026-02-30): got err=%v, want ErrInvalidDate", err)
	}
}

func TestNewDate_RejectsInvalidMonth(t *testing.T) {
	_, err := bizcal.NewDate(2026, time.Month(13), 1)
	if !errors.Is(err, bizcal.ErrInvalidDate) {
		t.Fatalf("NewDate(month=13): got err=%v, want ErrInvalidDate", err)
	}
}

func TestMustDate_PanicsOnInvalid(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("MustDate: expected panic on invalid date")
		}
	}()
	bizcal.MustDate(2026, time.February, 30)
}

func TestMustDate_ReturnsValid(t *testing.T) {
	d := bizcal.MustDate(2026, time.July, 21)
	if d.String() != "2026-07-21" {
		t.Fatalf("MustDate: got %s, want 2026-07-21", d.String())
	}
}

func TestDateOf_HonorsInstantOwnZone(t *testing.T) {
	kyiv, err := time.LoadLocation("Europe/Kyiv")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}

	// 2026-07-21T00:30 in Kyiv (UTC+3 in July) is 2026-07-20T21:30 UTC.
	// DateOf must read the instant in its own zone, so both must report July 21.
	kyivTime := time.Date(2026, time.July, 21, 0, 30, 0, 0, kyiv)
	utcTime := kyivTime.UTC()

	gotKyiv := bizcal.DateOf(kyivTime)
	wantKyiv := bizcal.MustDate(2026, time.July, 21)
	if gotKyiv != wantKyiv {
		t.Fatalf("DateOf(kyivTime) = %s, want %s", gotKyiv, wantKyiv)
	}

	gotUTC := bizcal.DateOf(utcTime)
	wantUTC := bizcal.MustDate(2026, time.July, 20)
	if gotUTC != wantUTC {
		t.Fatalf("DateOf(utcTime) = %s, want %s", gotUTC, wantUTC)
	}
}

func TestAddDays_AcrossMonthAndYearEnds(t *testing.T) {
	cases := []struct {
		start Date
		n     int
		want  Date
	}{
		{bizcal.MustDate(2026, time.January, 31), 1, bizcal.MustDate(2026, time.February, 1)},
		{bizcal.MustDate(2026, time.December, 31), 1, bizcal.MustDate(2027, time.January, 1)},
		{bizcal.MustDate(2027, time.January, 1), -1, bizcal.MustDate(2026, time.December, 31)},
		{bizcal.MustDate(2026, time.July, 21), 0, bizcal.MustDate(2026, time.July, 21)},
		{bizcal.MustDate(2028, time.March, 1), -1, bizcal.MustDate(2028, time.February, 29)}, // leap year
	}
	for _, c := range cases {
		got := c.start.AddDays(c.n)
		if got != c.want {
			t.Errorf("%s.AddDays(%d) = %s, want %s", c.start, c.n, got, c.want)
		}
	}
}

func TestWeekday_KnownDates(t *testing.T) {
	d := bizcal.MustDate(2026, time.July, 21)
	if d.Weekday() != time.Tuesday {
		t.Fatalf("Weekday(2026-07-21) = %v, want Tuesday", d.Weekday())
	}
}

func TestCompareBeforeAfter_Ordering(t *testing.T) {
	a := bizcal.MustDate(2026, time.July, 21)
	b := bizcal.MustDate(2026, time.July, 22)

	if !a.Before(b) {
		t.Fatal("a.Before(b) = false, want true")
	}
	if a.After(b) {
		t.Fatal("a.After(b) = true, want false")
	}
	if b.Before(a) {
		t.Fatal("b.Before(a) = true, want false")
	}
	if !b.After(a) {
		t.Fatal("b.After(a) = false, want true")
	}
	if a.Compare(b) >= 0 {
		t.Fatalf("a.Compare(b) = %d, want negative", a.Compare(b))
	}
	if b.Compare(a) <= 0 {
		t.Fatalf("b.Compare(a) = %d, want positive", b.Compare(a))
	}
	if a.Compare(a) != 0 {
		t.Fatalf("a.Compare(a) = %d, want 0", a.Compare(a))
	}
	if a.Before(a) || a.After(a) {
		t.Fatal("a.Before(a)/a.After(a) must both be false")
	}
}

func TestIsZero(t *testing.T) {
	var zero Date
	if !zero.IsZero() {
		t.Fatal("zero Date.IsZero() = false, want true")
	}
	d := bizcal.MustDate(2026, time.July, 21)
	if d.IsZero() {
		t.Fatal("non-zero Date.IsZero() = true, want false")
	}
}

func TestDate_String_ZeroPads(t *testing.T) {
	d := bizcal.MustDate(2026, time.January, 5)
	if d.String() != "2026-01-05" {
		t.Fatalf("String() = %s, want 2026-01-05", d.String())
	}
}

// Date is a local alias to bizcal.Date to keep table declarations concise.
type Date = bizcal.Date
