package i18n_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/core/i18n"
)

func TestDateTimeInvariant(t *testing.T) {
	t.Parallel()
	b := fmtBundle(t)
	en := b.Default()
	ts := time.Date(2026, 7, 17, 15, 4, 5, 0, time.UTC)

	// Invariant is ISO-8601: a neutral default, not a claim about English.
	assert.Equal(t, "2026-07-17", b.Date(en, ts))
	assert.Equal(t, "15:04", b.Time(en, ts))
	assert.Equal(t, "2026-07-17 15:04", b.DateTime(en, ts))
}

func TestDateTimeWiredSpec(t *testing.T) {
	t.Parallel()
	b := fmtBundle(t)
	de := b.ParseOrDefault("de")
	ts := time.Date(2026, 7, 17, 15, 4, 5, 0, time.UTC)

	assert.Equal(t, "17.07.2026", b.Date(de, ts))
	assert.Equal(t, "15:04", b.Time(de, ts))
	assert.Equal(t, "17.07.2026 15:04", b.DateTime(de, ts))
}

// TestPartialFormatSpecCompletes covers a partial FormatSpec: a caller who
// wires only the separators must still get real dates, not the empty string a
// zero DateLayout would feed time.AppendFormat. The empty layout fields fall
// back to Invariant while the separators the caller set are kept.
func TestPartialFormatSpecCompletes(t *testing.T) {
	t.Parallel()
	b := newBundle(t, i18n.WithFormat("de", i18n.FormatSpec{DecimalSep: ",", GroupSep: "."}))
	de := b.ParseOrDefault("de")
	ts := time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC)

	// Layouts were empty, so they default to Invariant — real output, never "".
	assert.Equal(t, "2026-01-02", b.Date(de, ts))
	assert.Equal(t, "15:04", b.Time(de, ts))
	assert.Equal(t, "2026-01-02 15:04", b.DateTime(de, ts))
	// The separators the caller did wire are preserved.
	assert.Equal(t, "1.234,5", b.Number(de, 1234.5))
}

func TestDateTimeUsesTimesOwnLocation(t *testing.T) {
	t.Parallel()
	b := fmtBundle(t)
	en := b.Default()
	kyiv := time.FixedZone("EEST", 3*3600)
	ts := time.Date(2026, 7, 17, 23, 30, 0, 0, time.UTC)

	// The package renders the time as given; converting is the caller's job.
	assert.Equal(t, "23:30", b.Time(en, ts))
	assert.Equal(t, "02:30", b.Time(en, ts.In(kyiv)))
	assert.Equal(t, "2026-07-18", b.Date(en, ts.In(kyiv)))
}

func TestDateTimeZeroLocale(t *testing.T) {
	t.Parallel()
	b := fmtBundle(t)
	var zero i18n.Locale
	ts := time.Date(2026, 1, 2, 3, 4, 0, 0, time.UTC)
	assert.Equal(t, "2026-01-02", b.Date(zero, ts))
}

// TestDateTimeZeroTime pins totality against the zero time.Time value: no
// wall clock, no location set. Rendering must never panic.
func TestDateTimeZeroTime(t *testing.T) {
	t.Parallel()
	b := fmtBundle(t)
	en := b.Default()
	var zero time.Time
	assert.Equal(t, "0001-01-01", b.Date(en, zero))
	assert.Equal(t, "00:00", b.Time(en, zero))
	assert.Equal(t, "0001-01-01 00:00", b.DateTime(en, zero))
}

func TestAppendDateZeroAlloc(t *testing.T) {
	// Must NOT run t.Parallel(): testing.AllocsPerRun panics in a parallel test.
	b := fmtBundle(t)
	en := b.Default()
	ts := time.Date(2026, 7, 17, 15, 4, 5, 0, time.UTC)
	dst := make([]byte, 0, 64)
	allocs := testing.AllocsPerRun(200, func() {
		_ = b.AppendDate(dst[:0], en, ts)
	})
	assert.Equal(t, 0.0, allocs, "AppendDate into a sized buffer must not allocate")
}

// TestAppendTimeZeroAlloc mirrors TestAppendDateZeroAlloc for AppendTime.
func TestAppendTimeZeroAlloc(t *testing.T) {
	b := fmtBundle(t)
	en := b.Default()
	ts := time.Date(2026, 7, 17, 15, 4, 5, 0, time.UTC)
	dst := make([]byte, 0, 16)
	allocs := testing.AllocsPerRun(200, func() {
		_ = b.AppendTime(dst[:0], en, ts)
	})
	assert.Equal(t, 0.0, allocs, "AppendTime into a sized buffer must not allocate")
}

// TestAppendDateTimeZeroAlloc mirrors TestAppendDateZeroAlloc for AppendDateTime.
func TestAppendDateTimeZeroAlloc(t *testing.T) {
	b := fmtBundle(t)
	en := b.Default()
	ts := time.Date(2026, 7, 17, 15, 4, 5, 0, time.UTC)
	dst := make([]byte, 0, 40)
	allocs := testing.AllocsPerRun(200, func() {
		_ = b.AppendDateTime(dst[:0], en, ts)
	})
	assert.Equal(t, 0.0, allocs, "AppendDateTime into a sized buffer must not allocate")
}

func TestAppendDateTimeAppends(t *testing.T) {
	t.Parallel()
	b := fmtBundle(t)
	en := b.Default()
	ts := time.Date(2026, 7, 17, 15, 4, 5, 0, time.UTC)
	assert.Equal(t, "x:2026-07-17", string(b.AppendDate([]byte("x:"), en, ts)))
	assert.Equal(t, "x:15:04", string(b.AppendTime([]byte("x:"), en, ts)))
	assert.Equal(t, "x:2026-07-17 15:04", string(b.AppendDateTime([]byte("x:"), en, ts)))
}
