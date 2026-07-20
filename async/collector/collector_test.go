package collector_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/async/collector"
)

// recordSink is a race-safe sink recording every Flush call.
type recordSink[T any] struct {
	mu      sync.Mutex
	batches [][]T
	ctxs    []context.Context
	err     error
	flushed chan struct{}
}

func newRecordSink[T any]() *recordSink[T] {
	return &recordSink[T]{flushed: make(chan struct{}, 64)}
}

func (s *recordSink[T]) Flush(ctx context.Context, batch []T) error {
	s.mu.Lock()
	s.batches = append(s.batches, batch)
	s.ctxs = append(s.ctxs, ctx)
	err := s.err
	s.mu.Unlock()
	select {
	case s.flushed <- struct{}{}:
	default:
	}
	return err
}

func (s *recordSink[T]) setErr(err error) {
	s.mu.Lock()
	s.err = err
	s.mu.Unlock()
}

func (s *recordSink[T]) snapshot() [][]T {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([][]T, len(s.batches))
	copy(out, s.batches)
	return out
}

func (s *recordSink[T]) waitFlush(t *testing.T) {
	t.Helper()
	select {
	case <-s.flushed:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for flush")
	}
}

// runCollector starts c.Run in a goroutine and returns a stop func that
// cancels it and waits for Run to return.
func runCollector[T any](t *testing.T, c *collector.Collector[T]) (stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()
	var once sync.Once
	stop = func() {
		once.Do(func() {
			cancel()
			select {
			case err := <-done:
				if err != nil {
					t.Errorf("Run returned %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Error("Run did not return after cancel")
			}
		})
	}
	t.Cleanup(stop)
	return stop
}

func TestNewValidation(t *testing.T) {
	t.Parallel()

	t.Run("nil sink", func(t *testing.T) {
		t.Parallel()
		_, err := collector.New[int](nil)
		if !errors.Is(err, collector.ErrNilSink) {
			t.Fatalf("got %v, want ErrNilSink", err)
		}
	})

	t.Run("invalid config", func(t *testing.T) {
		t.Parallel()
		bad := []collector.Config{
			{BufferSize: 0, BatchSize: 1, FlushInterval: time.Second},
			{BufferSize: 10, BatchSize: 0, FlushInterval: time.Second},
			{BufferSize: 10, BatchSize: 11, FlushInterval: time.Second},
			{BufferSize: 10, BatchSize: 5, FlushInterval: 0},
			{BufferSize: 10, BatchSize: 5, FlushInterval: time.Second, FlushTimeout: -1},
		}
		for _, cfg := range bad {
			_, err := collector.New(newRecordSink[int](), collector.WithConfig(cfg))
			if !errors.Is(err, collector.ErrInvalidConfig) {
				t.Errorf("config %+v: got %v, want ErrInvalidConfig", cfg, err)
			}
		}
	})

	t.Run("defaults valid", func(t *testing.T) {
		t.Parallel()
		if err := collector.DefaultConfig().Validate(); err != nil {
			t.Fatalf("DefaultConfig invalid: %v", err)
		}
	})
}

func TestName(t *testing.T) {
	t.Parallel()
	c, err := collector.New(newRecordSink[int]())
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Name(); got != "collector" {
		t.Fatalf("default name %q, want collector", got)
	}
	c2, err := collector.New(newRecordSink[int](), collector.WithName("clicks"))
	if err != nil {
		t.Fatal(err)
	}
	if got := c2.Name(); got != "clicks" {
		t.Fatalf("name %q, want clicks", got)
	}
}

func TestFlushBySize(t *testing.T) {
	t.Parallel()
	sink := newRecordSink[int]()
	c, err := collector.New(sink, collector.WithConfig(collector.Config{BufferSize: 16, BatchSize: 2, FlushInterval: time.Minute, FlushTimeout: time.Second}))
	if err != nil {
		t.Fatal(err)
	}
	runCollector(t, c)

	ctx := context.Background()
	for i := range 4 {
		if err := c.Add(ctx, i); err != nil {
			t.Fatalf("Add(%d): %v", i, err)
		}
	}
	sink.waitFlush(t)
	sink.waitFlush(t)

	batches := sink.snapshot()
	if len(batches) != 2 || len(batches[0]) != 2 || len(batches[1]) != 2 {
		t.Fatalf("batches %v, want two batches of two", batches)
	}
	if batches[0][0] != 0 || batches[0][1] != 1 || batches[1][0] != 2 || batches[1][1] != 3 {
		t.Fatalf("batches %v, want ordered [0 1] [2 3]", batches)
	}
	st := c.Stats()
	if st.Added != 4 || st.Flushed != 4 || st.Dropped != 0 || st.Lost != 0 {
		t.Fatalf("stats %+v", st)
	}
}

func TestFlushByAge(t *testing.T) {
	t.Parallel()
	sink := newRecordSink[string]()
	c, err := collector.New(sink, collector.WithConfig(collector.Config{BufferSize: 16, BatchSize: 10, FlushInterval: 20 * time.Millisecond, FlushTimeout: time.Second}))
	if err != nil {
		t.Fatal(err)
	}
	runCollector(t, c)

	if err := c.Add(context.Background(), "lonely"); err != nil {
		t.Fatal(err)
	}
	sink.waitFlush(t)

	batches := sink.snapshot()
	if len(batches) != 1 || len(batches[0]) != 1 || batches[0][0] != "lonely" {
		t.Fatalf("batches %v, want single [lonely]", batches)
	}
}

func TestDropNewestOnFullBuffer(t *testing.T) {
	t.Parallel()
	sink := newRecordSink[int]()
	c, err := collector.New(sink, collector.WithConfig(collector.Config{BufferSize: 2, BatchSize: 2, FlushInterval: time.Minute, FlushTimeout: time.Second}))
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	start := time.Now()
	if err := c.Add(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if err := c.Add(ctx, 2); err != nil {
		t.Fatal(err)
	}
	if err := c.Add(ctx, 3); !errors.Is(err, collector.ErrBufferFull) {
		t.Fatalf("third Add: got %v, want ErrBufferFull", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("Add blocked for %v, must be non-blocking", elapsed)
	}
	if st := c.Stats(); st.Added != 2 || st.Dropped != 1 {
		t.Fatalf("stats %+v, want Added=2 Dropped=1", st)
	}

	// The oldest events survive: draining flushes [1 2], never 3.
	runCollector(t, c)()
	batches := sink.snapshot()
	if len(batches) != 1 || len(batches[0]) != 2 || batches[0][0] != 1 || batches[0][1] != 2 {
		t.Fatalf("batches %v, want [1 2]", batches)
	}
}

func TestDrainOnShutdown(t *testing.T) {
	t.Parallel()
	sink := newRecordSink[int]()
	c, err := collector.New(sink, collector.WithConfig(collector.Config{BufferSize: 64, BatchSize: 10, FlushInterval: time.Minute, FlushTimeout: time.Second}))
	if err != nil {
		t.Fatal(err)
	}
	stop := runCollector(t, c)

	ctx := context.Background()
	for i := range 25 {
		if err := c.Add(ctx, i); err != nil {
			t.Fatalf("Add(%d): %v", i, err)
		}
	}
	stop()

	var total int
	for _, b := range sink.snapshot() {
		total += len(b)
	}
	if total != 25 {
		t.Fatalf("drained %d events, want 25", total)
	}
	if st := c.Stats(); st.Flushed != 25 {
		t.Fatalf("stats %+v, want Flushed=25", st)
	}
}

func TestAddAfterShutdown(t *testing.T) {
	t.Parallel()
	sink := newRecordSink[int]()
	c, err := collector.New(sink)
	if err != nil {
		t.Fatal(err)
	}
	runCollector(t, c)()

	if err := c.Add(context.Background(), 1); !errors.Is(err, collector.ErrClosed) {
		t.Fatalf("Add after shutdown: got %v, want ErrClosed", err)
	}
}

func TestSinkErrorLosesBatchAndRecovers(t *testing.T) {
	t.Parallel()
	sink := newRecordSink[int]()
	sink.setErr(errors.New("backend down"))
	c, err := collector.New(sink, collector.WithConfig(collector.Config{BufferSize: 16, BatchSize: 2, FlushInterval: time.Minute, FlushTimeout: time.Second}))
	if err != nil {
		t.Fatal(err)
	}
	runCollector(t, c)

	ctx := context.Background()
	if err := c.Add(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if err := c.Add(ctx, 2); err != nil {
		t.Fatal(err)
	}
	sink.waitFlush(t)
	if st := c.Stats(); st.Lost != 2 || st.Flushed != 0 {
		t.Fatalf("stats %+v, want Lost=2 Flushed=0", st)
	}

	// The flusher keeps running after a sink failure.
	sink.setErr(nil)
	if err := c.Add(ctx, 3); err != nil {
		t.Fatal(err)
	}
	if err := c.Add(ctx, 4); err != nil {
		t.Fatal(err)
	}
	sink.waitFlush(t)
	if st := c.Stats(); st.Lost != 2 || st.Flushed != 2 {
		t.Fatalf("stats %+v, want Lost=2 Flushed=2", st)
	}
}

func TestFlushTimeoutBoundsSink(t *testing.T) {
	t.Parallel()
	deadlineSeen := make(chan bool, 1)
	sink := collector.SinkFunc[int](func(ctx context.Context, _ []int) error {
		_, ok := ctx.Deadline()
		deadlineSeen <- ok
		<-ctx.Done()
		return ctx.Err()
	})
	c, err := collector.New(sink, collector.WithConfig(collector.Config{BufferSize: 4, BatchSize: 1, FlushInterval: time.Minute, FlushTimeout: 20 * time.Millisecond}))
	if err != nil {
		t.Fatal(err)
	}
	runCollector(t, c)

	if err := c.Add(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	select {
	case ok := <-deadlineSeen:
		if !ok {
			t.Fatal("sink context has no deadline, want FlushTimeout applied")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("sink never called")
	}
	waitFor(t, func() bool { return c.Stats().Lost == 1 })
}

func TestScopeFailClosed(t *testing.T) {
	t.Parallel()
	sink := newRecordSink[int]()
	hookErr := errors.New("no tenant")
	c, err := collector.New(sink, collector.WithScope(func(ctx context.Context) (string, error) {
		v, _ := ctx.Value(tenantKey{}).(string)
		if v == "boom" {
			return "", hookErr
		}
		return v, nil
	}))
	if err != nil {
		t.Fatal(err)
	}

	if err := c.Add(context.Background(), 1); !errors.Is(err, collector.ErrScopeMissing) {
		t.Fatalf("empty scope: got %v, want ErrScopeMissing", err)
	}
	err = c.Add(context.WithValue(context.Background(), tenantKey{}, "boom"), 1)
	if !errors.Is(err, collector.ErrScopeMissing) || !errors.Is(err, hookErr) {
		t.Fatalf("hook error: got %v, want ErrScopeMissing wrapping hook error", err)
	}
	if st := c.Stats(); st.Added != 0 {
		t.Fatalf("stats %+v, want Added=0", st)
	}
}

type tenantKey struct{}

func TestScopePartitioning(t *testing.T) {
	t.Parallel()
	sink := newRecordSink[string]()
	c, err := collector.New(sink,
		collector.WithConfig(collector.Config{BufferSize: 16, BatchSize: 10, FlushInterval: time.Minute, FlushTimeout: time.Second}),
		collector.WithScope(func(ctx context.Context) (string, error) {
			v, _ := ctx.Value(tenantKey{}).(string)
			return v, nil
		}),
		collector.WithScopeContext(func(ctx context.Context, scope string) context.Context {
			return context.WithValue(ctx, tenantKey{}, scope)
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	stop := runCollector(t, c)

	ctxA := context.WithValue(context.Background(), tenantKey{}, "a")
	ctxB := context.WithValue(context.Background(), tenantKey{}, "b")
	for _, add := range []struct {
		ctx context.Context
		ev  string
	}{{ctxA, "a1"}, {ctxB, "b1"}, {ctxA, "a2"}, {ctxB, "b2"}} {
		if err := c.Add(add.ctx, add.ev); err != nil {
			t.Fatalf("Add(%s): %v", add.ev, err)
		}
	}
	stop()

	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.batches) != 2 {
		t.Fatalf("flush calls %d, want 2 (one per scope)", len(sink.batches))
	}
	// First-seen scope order and per-scope event order both hold.
	if got := sink.batches[0]; len(got) != 2 || got[0] != "a1" || got[1] != "a2" {
		t.Fatalf("scope a batch %v, want [a1 a2]", got)
	}
	if got := sink.batches[1]; len(got) != 2 || got[0] != "b1" || got[1] != "b2" {
		t.Fatalf("scope b batch %v, want [b1 b2]", got)
	}
	for i, want := range []string{"a", "b"} {
		if got, _ := sink.ctxs[i].Value(tenantKey{}).(string); got != want {
			t.Fatalf("flush %d ctx scope %q, want %q", i, got, want)
		}
	}
}

func TestDropsLogged(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	var mu sync.Mutex
	log := slog.New(slog.NewTextHandler(lockedWriter{&mu, &buf}, nil))
	sink := newRecordSink[int]()
	c, err := collector.New(sink,
		collector.WithConfig(collector.Config{BufferSize: 1, BatchSize: 1, FlushInterval: 20 * time.Millisecond, FlushTimeout: time.Second}),
		collector.WithLogger(log),
	)
	if err != nil {
		t.Fatal(err)
	}

	// Fill the buffer and force a drop before the flusher starts.
	ctx := context.Background()
	if err := c.Add(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if err := c.Add(ctx, 2); !errors.Is(err, collector.ErrBufferFull) {
		t.Fatalf("got %v, want ErrBufferFull", err)
	}
	runCollector(t, c)

	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return strings.Contains(buf.String(), "collector dropped events")
	})
}

type lockedWriter struct {
	mu *sync.Mutex
	w  *bytes.Buffer
}

func (l lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

func TestConcurrentAdd(t *testing.T) {
	t.Parallel()
	sink := newRecordSink[int]()
	c, err := collector.New(sink, collector.WithConfig(collector.Config{BufferSize: 4096, BatchSize: 128, FlushInterval: 5 * time.Millisecond, FlushTimeout: time.Second}))
	if err != nil {
		t.Fatal(err)
	}
	stop := runCollector(t, c)

	const goroutines, perG = 8, 500
	var wg sync.WaitGroup
	for g := range goroutines {
		wg.Go(func() {
			ctx := context.Background()
			for i := range perG {
				_ = c.Add(ctx, g*perG+i)
			}
		})
	}
	wg.Wait()
	stop()

	var total int
	for _, b := range sink.snapshot() {
		total += len(b)
	}
	st := c.Stats()
	if uint64(total) != st.Flushed {
		t.Fatalf("sink saw %d events, stats say Flushed=%d", total, st.Flushed)
	}
	if st.Added+st.Dropped != goroutines*perG {
		t.Fatalf("stats %+v, Added+Dropped want %d", st, goroutines*perG)
	}
	if st.Added != st.Flushed {
		t.Fatalf("stats %+v, every accepted event must be flushed after drain", st)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition never became true")
}
