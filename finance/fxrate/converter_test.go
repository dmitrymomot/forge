package fxrate_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/core/decimal"
	"github.com/dmitrymomot/forge/finance/fxrate"
)

// fakeSource is a scriptable RateSource: it returns the queued results in
// order and records every call.
type fakeSource struct {
	mu        sync.Mutex
	calls     int
	results   []func() (fxrate.Snapshot, error)
	gotBase   string
	gotQuotes []string
	delay     time.Duration
}

func (f *fakeSource) Fetch(_ context.Context, base string, quotes []string) (fxrate.Snapshot, error) {
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.gotBase = base
	f.gotQuotes = quotes
	next := f.results[0]
	if len(f.results) > 1 {
		f.results = f.results[1:]
	}
	return next()
}

func (f *fakeSource) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func snapResult(s fxrate.Snapshot) func() (fxrate.Snapshot, error) {
	return func() (fxrate.Snapshot, error) { return s, nil }
}

func errResult(err error) func() (fxrate.Snapshot, error) {
	return func() (fxrate.Snapshot, error) { return fxrate.Snapshot{}, err }
}

func testSnapshot(t *testing.T) fxrate.Snapshot {
	t.Helper()
	return mustSnapshot(t, "EUR", map[string]decimal.Decimal{"USD": d("1.0850"), "GBP": d("0.8425")})
}

func TestNewValidation(t *testing.T) {
	t.Parallel()

	src := &fakeSource{results: []func() (fxrate.Snapshot, error){errResult(errors.New("unused"))}}
	tests := []struct {
		name   string
		source fxrate.RateSource
		base   string
		opts   []fxrate.Option
	}{
		{"nil source", nil, "EUR", nil},
		{"empty base", src, "  ", nil},
		{"non-positive TTL", src, "EUR", []fxrate.Option{fxrate.WithTTL(0)}},
		{"nil clock", src, "EUR", []fxrate.Option{fxrate.WithClock(nil)}},
		{"empty quote", src, "EUR", []fxrate.Option{fxrate.WithQuotes("USD", " ")}},
		{"quote equals base", src, "EUR", []fxrate.Option{fxrate.WithQuotes("eur")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := fxrate.New(tt.source, tt.base, tt.opts...); err == nil {
				t.Fatal("New succeeded, want error")
			}
		})
	}
}

func TestConverterCachesWithinTTL(t *testing.T) {
	t.Parallel()

	snap := testSnapshot(t)
	src := &fakeSource{results: []func() (fxrate.Snapshot, error){snapResult(snap)}}
	clk := clock.NewMock(asOf)
	conv, err := fxrate.New(src, "eur", fxrate.WithTTL(time.Hour), fxrate.WithClock(clk), fxrate.WithQuotes("usd", "GBP"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	c, err := conv.Convert(t.Context(), d("100.00"), "EUR", "USD", 2, decimal.HalfEven)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if got := c.Result.String(); got != "108.50" {
		t.Fatalf("Result = %s, want 108.50", got)
	}
	if src.gotBase != "EUR" {
		t.Fatalf("source got base %q, want EUR", src.gotBase)
	}
	if len(src.gotQuotes) != 2 || src.gotQuotes[0] != "USD" || src.gotQuotes[1] != "GBP" {
		t.Fatalf("source got quotes %v, want [USD GBP]", src.gotQuotes)
	}

	// Within the TTL every call serves the cache.
	clk.Advance(59 * time.Minute)
	if _, err := conv.Rate(t.Context(), "USD", "GBP"); err != nil {
		t.Fatalf("Rate: %v", err)
	}
	if _, err := conv.Snapshot(t.Context()); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if got := src.callCount(); got != 1 {
		t.Fatalf("source called %d times, want 1", got)
	}

	// Past the TTL the next call refreshes.
	clk.Advance(2 * time.Minute)
	if _, err := conv.Convert(t.Context(), d("1"), "EUR", "USD", 2, decimal.HalfEven); err != nil {
		t.Fatalf("Convert after TTL: %v", err)
	}
	if got := src.callCount(); got != 2 {
		t.Fatalf("source called %d times, want 2", got)
	}
}

func TestConverterFailsClosedOnFetchError(t *testing.T) {
	t.Parallel()

	cause := errors.New("provider down")
	src := &fakeSource{results: []func() (fxrate.Snapshot, error){errResult(cause)}}
	conv, err := fxrate.New(src, "EUR", fxrate.WithClock(clock.NewMock(asOf)))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = conv.Convert(t.Context(), d("1"), "EUR", "USD", 2, decimal.HalfEven)
	if !errors.Is(err, fxrate.ErrFetchFailed) {
		t.Fatalf("got %v, want ErrFetchFailed", err)
	}
	if !errors.Is(err, cause) {
		t.Fatalf("cause not joined: %v", err)
	}
}

func TestConverterStalePlusFailingSourceIsAnError(t *testing.T) {
	t.Parallel()

	snap := testSnapshot(t)
	cause := errors.New("provider down")
	src := &fakeSource{results: []func() (fxrate.Snapshot, error){snapResult(snap), errResult(cause)}}
	clk := clock.NewMock(asOf)
	conv, err := fxrate.New(src, "EUR", fxrate.WithTTL(time.Hour), fxrate.WithClock(clk))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := conv.Snapshot(t.Context()); err != nil {
		t.Fatalf("warm-up: %v", err)
	}

	// Once stale, a failing source surfaces as an error — the stale snapshot
	// is never served silently.
	clk.Advance(2 * time.Hour)
	if _, err := conv.Convert(t.Context(), d("1"), "EUR", "USD", 2, decimal.HalfEven); !errors.Is(err, fxrate.ErrFetchFailed) {
		t.Fatalf("got %v, want ErrFetchFailed", err)
	}
}

func TestConverterRejectsBadSourceResults(t *testing.T) {
	t.Parallel()

	wrongBase := mustSnapshot(t, "USD", map[string]decimal.Decimal{"EUR": d("0.92")})
	missingQuote := mustSnapshot(t, "EUR", map[string]decimal.Decimal{"USD": d("1.0850")})

	tests := []struct {
		name   string
		result func() (fxrate.Snapshot, error)
		opts   []fxrate.Option
		want   error
	}{
		{"zero snapshot nil error", snapResult(fxrate.Snapshot{}), nil, fxrate.ErrFetchFailed},
		{"base mismatch", snapResult(wrongBase), nil, fxrate.ErrBaseMismatch},
		{"omitted requested quote", snapResult(missingQuote), []fxrate.Option{fxrate.WithQuotes("USD", "GBP")}, fxrate.ErrFetchFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			src := &fakeSource{results: []func() (fxrate.Snapshot, error){tt.result}}
			conv, err := fxrate.New(src, "EUR", append(tt.opts, fxrate.WithClock(clock.NewMock(asOf)))...)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if _, err := conv.Snapshot(t.Context()); !errors.Is(err, tt.want) {
				t.Fatalf("got %v, want %v", err, tt.want)
			}
		})
	}
}

func TestConverterFollowerWaitBoundedByOwnContext(t *testing.T) {
	t.Parallel()

	snap := testSnapshot(t)
	src := &fakeSource{results: []func() (fxrate.Snapshot, error){snapResult(snap)}, delay: 400 * time.Millisecond}
	conv, err := fxrate.New(src, "EUR", fxrate.WithClock(clock.NewMock(asOf)))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	leaderDone := make(chan error, 1)
	go func() {
		_, err := conv.Snapshot(context.Background())
		leaderDone <- err
	}()
	time.Sleep(50 * time.Millisecond) // let the leader's slow fetch start

	// A caller with a short deadline joining the in-flight refresh must be
	// released by its own context, not pinned until the leader's fetch ends.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = conv.Snapshot(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("follower got %v, want context.DeadlineExceeded", err)
	}
	if elapsed := time.Since(start); elapsed > 300*time.Millisecond {
		t.Fatalf("follower blocked %v — pinned to the leader's fetch", elapsed)
	}

	if err := <-leaderDone; err != nil {
		t.Fatalf("leader: %v", err)
	}
}

func TestConverterCoalescesConcurrentRefreshes(t *testing.T) {
	t.Parallel()

	snap := testSnapshot(t)
	src := &fakeSource{results: []func() (fxrate.Snapshot, error){snapResult(snap)}, delay: 50 * time.Millisecond}
	conv, err := fxrate.New(src, "EUR", fxrate.WithClock(clock.NewMock(asOf)))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	const workers = 16
	var wg sync.WaitGroup
	errs := make([]error, workers)
	for i := range workers {
		wg.Go(func() {
			_, errs[i] = conv.Convert(t.Context(), d("1"), "EUR", "USD", 2, decimal.HalfEven)
		})
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("worker %d: %v", i, err)
		}
	}
	if got := src.callCount(); got != 1 {
		t.Fatalf("source called %d times, want 1 (singleflight)", got)
	}
}
