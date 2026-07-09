package logsample_test

import (
	"context"
	"log/slog"
	"sync"
	"testing"

	"github.com/dmitrymomot/forge/ops/logsample"
)

// capture is a test-double slog.Handler that counts records reaching it.
type capture struct {
	mu      sync.Mutex
	records int
}

func (c *capture) Enabled(context.Context, slog.Level) bool { return true }
func (c *capture) Handle(_ context.Context, _ slog.Record) error {
	c.mu.Lock()
	c.records++
	c.mu.Unlock()
	return nil
}
func (c *capture) WithAttrs([]slog.Attr) slog.Handler { return c }
func (c *capture) WithGroup(string) slog.Handler      { return c }
func (c *capture) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.records
}

func TestSamplesSubThreshold(t *testing.T) {
	cap := &capture{}
	log := slog.New(logsample.New(cap, logsample.WithRate(10), logsample.WithMinLevel(slog.LevelWarn)))
	for range 100 {
		log.Info("noise")
	}
	if got := cap.count(); got != 10 {
		t.Fatalf("kept %d Info records, want 10", got)
	}
}

func TestAlwaysPassesAtOrAboveThreshold(t *testing.T) {
	cap := &capture{}
	log := slog.New(logsample.New(cap, logsample.WithRate(10)))
	for range 50 {
		log.Warn("important")
	}
	if got := cap.count(); got != 50 {
		t.Fatalf("kept %d Warn records, want 50", got)
	}
}

func TestRateOneKeepsEverything(t *testing.T) {
	cap := &capture{}
	log := slog.New(logsample.New(cap, logsample.WithRate(1)))
	for range 20 {
		log.Info("x")
	}
	if got := cap.count(); got != 20 {
		t.Fatalf("kept %d, want 20", got)
	}
}

func TestDerivedHandlerSharesCounter(t *testing.T) {
	cap := &capture{}
	log := slog.New(logsample.New(cap, logsample.WithRate(10)))
	child := log.With("k", "v")
	// 5 via parent + 5 via child interleaved = 10 sub-threshold records; at rate 10
	// a shared counter keeps exactly the 1st (=> 1). A per-handler counter would
	// keep the 1st of each => 2.
	for range 5 {
		log.Info("a")
		child.Info("b")
	}
	if got := cap.count(); got != 1 {
		t.Fatalf("kept %d, want 1 (counter must be shared across derived handlers)", got)
	}
}

func TestConcurrentHandleIsDeterministic(t *testing.T) {
	cap := &capture{}
	log := slog.New(logsample.New(cap, logsample.WithRate(4)))
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 100 {
				log.Info("x")
			}
		})
	}
	wg.Wait()
	// 800 sub-threshold records at rate 4 => (n-1)%4==0 for n=1,5,...,797 => 200.
	if got := cap.count(); got != 200 {
		t.Fatalf("kept %d, want 200", got)
	}
}
