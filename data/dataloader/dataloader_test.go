package dataloader_test

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/data/dataloader"
)

// countingFetch records every batch it receives and resolves keys via fn.
type countingFetch struct {
	mu      sync.Mutex
	batches [][]string
	fn      func(ctx context.Context, keys []string) (map[string]string, error)
}

func (c *countingFetch) fetch(ctx context.Context, keys []string) (map[string]string, error) {
	c.mu.Lock()
	c.batches = append(c.batches, append([]string(nil), keys...))
	c.mu.Unlock()
	if c.fn != nil {
		return c.fn(ctx, keys)
	}
	out := make(map[string]string, len(keys))
	for _, k := range keys {
		out[k] = "v:" + k
	}
	return out, nil
}

func (c *countingFetch) calls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.batches)
}

func (c *countingFetch) batchSizes() []int {
	c.mu.Lock()
	defer c.mu.Unlock()
	sizes := make([]int, len(c.batches))
	for i, b := range c.batches {
		sizes[i] = len(b)
	}
	return sizes
}

func TestLoadSingle(t *testing.T) {
	t.Parallel()
	cf := &countingFetch{}
	l := dataloader.New(cf.fetch, dataloader.WithMaxBatchSize(1))

	v, err := l.Load(t.Context(), "a")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if v != "v:a" {
		t.Fatalf("Load = %q, want %q", v, "v:a")
	}
	if got := cf.calls(); got != 1 {
		t.Fatalf("fetch calls = %d, want 1", got)
	}
}

func TestLoadNotFound(t *testing.T) {
	t.Parallel()
	l := dataloader.New(func(_ context.Context, _ []string) (map[string]string, error) {
		return nil, nil
	}, dataloader.WithMaxBatchSize(1))

	_, err := l.Load(t.Context(), "missing")
	if !errors.Is(err, dataloader.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Fatalf("err %q does not name the key", err)
	}
}

func TestLoadCoalescesConcurrent(t *testing.T) {
	t.Parallel()
	const n = 10
	cf := &countingFetch{}
	// Window effectively infinite; the size cap is the only trigger, so the
	// test is deterministic: fetch fires exactly when all n keys are in. The
	// deadline turns a coalescing regression into a fast failure instead of a
	// suite-timeout hang.
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	l := dataloader.New(cf.fetch, dataloader.WithWait(time.Hour), dataloader.WithMaxBatchSize(n))

	var wg sync.WaitGroup
	errs := make([]error, n)
	vals := make([]string, n)
	for i := range n {
		wg.Go(func() {
			vals[i], errs[i] = l.Load(ctx, string(rune('a'+i)))
		})
	}
	wg.Wait()

	for i := range n {
		if errs[i] != nil {
			t.Fatalf("Load %d: %v", i, errs[i])
		}
		if want := "v:" + string(rune('a'+i)); vals[i] != want {
			t.Fatalf("Load %d = %q, want %q", i, vals[i], want)
		}
	}
	if got := cf.calls(); got != 1 {
		t.Fatalf("fetch calls = %d, want 1", got)
	}
}

func TestLoadDedupsInFlightKey(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	var calls atomic.Int32
	l := dataloader.New(func(_ context.Context, keys []string) (map[string]string, error) {
		calls.Add(1)
		<-release
		return map[string]string{keys[0]: "shared"}, nil
	}, dataloader.WithMaxBatchSize(1))

	results := make(chan string, 2)
	for range 2 {
		go func() {
			v, err := l.Load(t.Context(), "k")
			if err != nil {
				t.Errorf("Load: %v", err)
			}
			results <- v
		}()
	}
	// Both goroutines target the same key; the second joins the first's
	// in-flight thunk. Let the fetch finish once both are waiting.
	time.Sleep(10 * time.Millisecond)
	close(release)
	for range 2 {
		if v := <-results; v != "shared" {
			t.Fatalf("Load = %q, want %q", v, "shared")
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("fetch calls = %d, want 1", got)
	}
}

func TestLoadCachedSkipsFetch(t *testing.T) {
	t.Parallel()
	cf := &countingFetch{}
	l := dataloader.New(cf.fetch, dataloader.WithMaxBatchSize(1))

	for range 3 {
		if _, err := l.Load(t.Context(), "a"); err != nil {
			t.Fatalf("Load: %v", err)
		}
	}
	if got := cf.calls(); got != 1 {
		t.Fatalf("fetch calls = %d, want 1", got)
	}
}

func TestLoadManySingleBatch(t *testing.T) {
	t.Parallel()
	cf := &countingFetch{}
	keys := []string{"a", "b", "c", "a", "b"} // duplicates collapse
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	l := dataloader.New(cf.fetch, dataloader.WithWait(time.Hour), dataloader.WithMaxBatchSize(3))

	got, err := l.LoadMany(ctx, keys)
	if err != nil {
		t.Fatalf("LoadMany: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("LoadMany returned %d entries, want 3", len(got))
	}
	for _, k := range []string{"a", "b", "c"} {
		if got[k] != "v:"+k {
			t.Fatalf("LoadMany[%q] = %q, want %q", k, got[k], "v:"+k)
		}
	}
	if got := cf.calls(); got != 1 {
		t.Fatalf("fetch calls = %d, want 1", got)
	}
}

func TestLoadManyMissingKeysAbsent(t *testing.T) {
	t.Parallel()
	l := dataloader.New(func(_ context.Context, keys []string) (map[string]string, error) {
		out := make(map[string]string)
		for _, k := range keys {
			if k != "gone" {
				out[k] = "v:" + k
			}
		}
		return out, nil
	}, dataloader.WithMaxBatchSize(3))

	got, err := l.LoadMany(t.Context(), []string{"a", "gone", "b"})
	if err != nil {
		t.Fatalf("LoadMany: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("LoadMany returned %d entries, want 2", len(got))
	}
	if _, ok := got["gone"]; ok {
		t.Fatal("missing key present in result map")
	}
}

func TestLoadManyBatchErrorDeduplicated(t *testing.T) {
	t.Parallel()
	boom := errors.New("db down")
	l := dataloader.New(func(_ context.Context, _ []string) (map[string]string, error) {
		return nil, boom
	}, dataloader.WithMaxBatchSize(5))

	got, err := l.LoadMany(t.Context(), []string{"a", "b", "c", "d", "e"})
	if len(got) != 0 {
		t.Fatalf("LoadMany returned %d entries, want 0", len(got))
	}
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want wrapped %v", err, boom)
	}
	// One batch failed once; the joined error must not repeat it per key.
	if n := strings.Count(err.Error(), "db down"); n != 1 {
		t.Fatalf("error mentions batch failure %d times, want 1: %v", n, err)
	}
}

func TestMaxBatchSplits(t *testing.T) {
	t.Parallel()
	cf := &countingFetch{}
	l := dataloader.New(cf.fetch, dataloader.WithWait(5*time.Millisecond), dataloader.WithMaxBatchSize(10))

	keys := make([]string, 25)
	for i := range keys {
		keys[i] = string(rune('a' + i))
	}
	got, err := l.LoadMany(t.Context(), keys)
	if err != nil {
		t.Fatalf("LoadMany: %v", err)
	}
	if len(got) != 25 {
		t.Fatalf("LoadMany returned %d entries, want 25", len(got))
	}
	// LoadMany schedules all keys under one lock hold, so the split is
	// deterministic: two full batches plus a 5-key remainder on the timer.
	sizes := cf.batchSizes()
	if len(sizes) != 3 || sizes[0] != 10 || sizes[1] != 10 || sizes[2] != 5 {
		t.Fatalf("batch sizes = %v, want [10 10 5]", sizes)
	}
}

func TestErrorCachedUntilClear(t *testing.T) {
	t.Parallel()
	boom := errors.New("transient")
	var calls atomic.Int32
	l := dataloader.New(func(_ context.Context, keys []string) (map[string]string, error) {
		if calls.Add(1) == 1 {
			return nil, boom
		}
		return map[string]string{keys[0]: "recovered"}, nil
	}, dataloader.WithMaxBatchSize(1))

	if _, err := l.Load(t.Context(), "k"); !errors.Is(err, boom) {
		t.Fatalf("first Load err = %v, want %v", err, boom)
	}
	// The failure is memoized: no refetch without Clear.
	if _, err := l.Load(t.Context(), "k"); !errors.Is(err, boom) {
		t.Fatalf("second Load err = %v, want cached %v", err, boom)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("fetch calls = %d, want 1", got)
	}

	l.Clear("k")
	v, err := l.Load(t.Context(), "k")
	if err != nil {
		t.Fatalf("Load after Clear: %v", err)
	}
	if v != "recovered" {
		t.Fatalf("Load after Clear = %q, want %q", v, "recovered")
	}
}

func TestFetchPanicRecovered(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	l := dataloader.New(func(_ context.Context, keys []string) (map[string]string, error) {
		if calls.Add(1) == 1 {
			panic("kaboom")
		}
		return map[string]string{keys[0]: "ok"}, nil
	}, dataloader.WithMaxBatchSize(1))

	_, err := l.Load(t.Context(), "a")
	if !errors.Is(err, dataloader.ErrFetchPanic) {
		t.Fatalf("err = %v, want ErrFetchPanic", err)
	}
	if !strings.Contains(err.Error(), "kaboom") {
		t.Fatalf("err %q does not carry the panic value", err)
	}

	// The loader stays usable after a fetch panic.
	v, err := l.Load(t.Context(), "b")
	if err != nil {
		t.Fatalf("Load after panic: %v", err)
	}
	if v != "ok" {
		t.Fatalf("Load after panic = %q, want %q", v, "ok")
	}
}

func TestPrime(t *testing.T) {
	t.Parallel()
	cf := &countingFetch{}
	l := dataloader.New(cf.fetch, dataloader.WithMaxBatchSize(1))

	if !l.Prime("a", "seeded") {
		t.Fatal("Prime new key = false, want true")
	}
	if l.Prime("a", "overwritten") {
		t.Fatal("Prime existing key = true, want false")
	}

	v, err := l.Load(t.Context(), "a")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if v != "seeded" {
		t.Fatalf("Load = %q, want primed %q", v, "seeded")
	}
	if got := cf.calls(); got != 0 {
		t.Fatalf("fetch calls = %d, want 0", got)
	}
}

func TestClearAll(t *testing.T) {
	t.Parallel()
	cf := &countingFetch{}
	l := dataloader.New(cf.fetch, dataloader.WithMaxBatchSize(2))

	if _, err := l.LoadMany(t.Context(), []string{"a", "b"}); err != nil {
		t.Fatalf("LoadMany: %v", err)
	}
	l.ClearAll()
	if _, err := l.LoadMany(t.Context(), []string{"a", "b"}); err != nil {
		t.Fatalf("LoadMany after ClearAll: %v", err)
	}
	if got := cf.calls(); got != 2 {
		t.Fatalf("fetch calls = %d, want 2", got)
	}
}

func TestCanceledWaitDoesNotAbortBatch(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	l := dataloader.New(func(_ context.Context, keys []string) (map[string]string, error) {
		<-release
		return map[string]string{keys[0]: "late"}, nil
	}, dataloader.WithMaxBatchSize(1))

	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()
	if _, err := l.Load(ctx, "k"); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}

	// The batch keeps running and its result lands in the cache.
	close(release)
	v, err := l.Load(t.Context(), "k")
	if err != nil {
		t.Fatalf("Load after cancel: %v", err)
	}
	if v != "late" {
		t.Fatalf("Load after cancel = %q, want %q", v, "late")
	}
}

func TestFetchContextDetachedButCarriesValues(t *testing.T) {
	t.Parallel()
	type ctxKey struct{}
	release := make(chan struct{})
	fetchErr := make(chan error, 1)
	fetchVal := make(chan any, 1)
	l := dataloader.New(func(ctx context.Context, keys []string) (map[string]string, error) {
		<-release
		fetchErr <- ctx.Err()
		fetchVal <- ctx.Value(ctxKey{})
		return map[string]string{keys[0]: "ok"}, nil
	}, dataloader.WithMaxBatchSize(1))

	ctx, cancel := context.WithCancel(context.WithValue(t.Context(), ctxKey{}, "tenant-1"))
	done := make(chan struct{})
	go func() {
		_, _ = l.Load(ctx, "k")
		close(done)
	}()
	time.Sleep(5 * time.Millisecond)
	cancel()
	close(release)
	<-done

	if err := <-fetchErr; err != nil {
		t.Fatalf("fetch ctx canceled: %v (must be detached from caller cancellation)", err)
	}
	if v := <-fetchVal; v != "tenant-1" {
		t.Fatalf("fetch ctx value = %v, want %q", v, "tenant-1")
	}
}

func TestZeroValueLoaderErrors(t *testing.T) {
	t.Parallel()
	var l dataloader.Loader[string, string]
	if _, err := l.Load(t.Context(), "k"); err == nil {
		t.Fatal("zero-value Load = nil error, want error")
	}
	if _, err := l.LoadMany(t.Context(), []string{"k"}); err == nil {
		t.Fatal("zero-value LoadMany = nil error, want error")
	}
	if l.Prime("k", "v") {
		t.Fatal("zero-value Prime = true, want false")
	}
	l.Clear("k") // must not panic
	l.ClearAll() // must not panic

	var nilLoader *dataloader.Loader[string, string]
	if _, err := nilLoader.Load(t.Context(), "k"); err == nil {
		t.Fatal("nil-loader Load = nil error, want error")
	}
	if _, err := nilLoader.LoadMany(t.Context(), []string{"k"}); err == nil {
		t.Fatal("nil-loader LoadMany = nil error, want error")
	}
	if nilLoader.Prime("k", "v") {
		t.Fatal("nil-loader Prime = true, want false")
	}
	nilLoader.Clear("k") // must not panic
	nilLoader.ClearAll() // must not panic
}

func TestNewNilFetchPanics(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Fatal("New(nil) did not panic")
		}
	}()
	dataloader.New[string, string](nil)
}

func TestWaitWindowCoalescesAcrossGoroutines(t *testing.T) {
	t.Parallel()
	cf := &countingFetch{}
	// A generous window: the assertion only needs both goroutines to start
	// within 1s of each other, so scheduler jitter on slow CI cannot flake it.
	l := dataloader.New(cf.fetch, dataloader.WithWait(time.Second))

	var wg sync.WaitGroup
	for _, k := range []string{"a", "b"} {
		wg.Go(func() {
			if _, err := l.Load(t.Context(), k); err != nil {
				t.Errorf("Load(%q): %v", k, err)
			}
		})
	}
	wg.Wait()
	if got := cf.calls(); got != 1 {
		t.Fatalf("fetch calls = %d, want 1 (both keys inside one window)", got)
	}
}

func TestClearDuringOpenBatchNeverDuplicatesKeys(t *testing.T) {
	t.Parallel()
	cf := &countingFetch{}
	l := dataloader.New(cf.fetch, dataloader.WithWait(20*time.Millisecond), dataloader.WithMaxBatchSize(2))

	var wg sync.WaitGroup
	wg.Go(func() {
		if _, err := l.Load(t.Context(), "k"); err != nil {
			t.Errorf("Load k#1: %v", err)
		}
	})
	time.Sleep(5 * time.Millisecond) // let k land in the open batch
	l.Clear("k")
	for _, key := range []string{"k", "x"} {
		wg.Go(func() {
			if _, err := l.Load(t.Context(), key); err != nil {
				t.Errorf("Load %s: %v", key, err)
			}
		})
	}
	wg.Wait()

	// Same interleaving through ClearAll.
	wg.Go(func() {
		if _, err := l.Load(t.Context(), "m"); err != nil {
			t.Errorf("Load m#1: %v", err)
		}
	})
	time.Sleep(5 * time.Millisecond)
	l.ClearAll()
	for _, key := range []string{"m", "n"} {
		wg.Go(func() {
			if _, err := l.Load(t.Context(), key); err != nil {
				t.Errorf("Load %s: %v", key, err)
			}
		})
	}
	wg.Wait()

	// The contract under test: no single fetch may ever see the same key
	// twice, regardless of how Clear interleaved with the open batch.
	cf.mu.Lock()
	defer cf.mu.Unlock()
	for _, batch := range cf.batches {
		seen := make(map[string]bool, len(batch))
		for _, k := range batch {
			if seen[k] {
				t.Fatalf("fetch received duplicate key %q in batch %v", k, batch)
			}
			seen[k] = true
		}
	}
}

func TestLoadManySurfacesBatchErrorWrappingNotFound(t *testing.T) {
	t.Parallel()
	batchErr := fmt.Errorf("upstream 404: %w", dataloader.ErrNotFound)
	l := dataloader.New(func(_ context.Context, _ []string) (map[string]string, error) {
		return nil, batchErr
	}, dataloader.WithMaxBatchSize(2))

	got, err := l.LoadMany(t.Context(), []string{"a", "b"})
	if len(got) != 0 {
		t.Fatalf("LoadMany returned %d entries, want 0", len(got))
	}
	// A whole-batch failure must never be reinterpreted as per-key absence,
	// even when the caller's error wraps ErrNotFound.
	if !errors.Is(err, batchErr) {
		t.Fatalf("err = %v, want wrapped %v", err, batchErr)
	}
}

func TestCachedHitWinsOverCanceledContext(t *testing.T) {
	t.Parallel()
	l := dataloader.New((&countingFetch{}).fetch, dataloader.WithMaxBatchSize(1))
	if _, err := l.Load(t.Context(), "k"); err != nil {
		t.Fatalf("warm Load: %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	v, err := l.Load(ctx, "k")
	if err != nil {
		t.Fatalf("cached Load under canceled ctx = %v, want value", err)
	}
	if v != "v:k" {
		t.Fatalf("cached Load = %q, want %q", v, "v:k")
	}
}

func TestUnboundedBatchFiresOnTimer(t *testing.T) {
	t.Parallel()
	cf := &countingFetch{}
	l := dataloader.New(cf.fetch, dataloader.WithWait(10*time.Millisecond), dataloader.WithMaxBatchSize(0))

	keys := make([]string, 500)
	for i := range keys {
		keys[i] = strconv.Itoa(i)
	}
	got, err := l.LoadMany(t.Context(), keys)
	if err != nil {
		t.Fatalf("LoadMany: %v", err)
	}
	if len(got) != 500 {
		t.Fatalf("LoadMany returned %d entries, want 500", len(got))
	}
	if calls := cf.calls(); calls != 1 {
		t.Fatalf("fetch calls = %d, want 1 (uncapped single batch)", calls)
	}
}

func TestLoadManyEmptyKeys(t *testing.T) {
	t.Parallel()
	cf := &countingFetch{}
	l := dataloader.New(cf.fetch)

	got, err := l.LoadMany(t.Context(), nil)
	if err != nil {
		t.Fatalf("LoadMany(nil): %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("LoadMany(nil) returned %d entries, want 0", len(got))
	}
	if calls := cf.calls(); calls != 0 {
		t.Fatalf("fetch calls = %d, want 0", calls)
	}
}

func TestExtraUnrequestedKeysIgnored(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	l := dataloader.New(func(_ context.Context, keys []string) (map[string]string, error) {
		calls.Add(1)
		out := map[string]string{"extra": "smuggled"}
		for _, k := range keys {
			out[k] = "v:" + k
		}
		return out, nil
	}, dataloader.WithMaxBatchSize(1))

	if _, err := l.Load(t.Context(), "a"); err != nil {
		t.Fatalf("Load: %v", err)
	}
	// The unrequested "extra" key must not have been cached.
	v, err := l.Load(t.Context(), "extra")
	if err != nil {
		t.Fatalf("Load extra: %v", err)
	}
	if v != "v:extra" {
		t.Fatalf("Load extra = %q, want freshly fetched %q", v, "v:extra")
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("fetch calls = %d, want 2 (extra key not cached)", got)
	}
}

func TestNonStringInstantiation(t *testing.T) {
	t.Parallel()
	type row struct {
		Name string
		Tags []string
	}
	l := dataloader.New(func(_ context.Context, ids []int) (map[int]*row, error) {
		out := make(map[int]*row, len(ids))
		for _, id := range ids {
			out[id] = &row{Name: strconv.Itoa(id), Tags: []string{"t"}}
		}
		return out, nil
	}, dataloader.WithMaxBatchSize(2))

	got, err := l.LoadMany(t.Context(), []int{7, 42})
	if err != nil {
		t.Fatalf("LoadMany: %v", err)
	}
	if got[7] == nil || got[7].Name != "7" || got[42] == nil || got[42].Name != "42" {
		t.Fatalf("LoadMany = %+v, want rows for 7 and 42", got)
	}
}

func TestConcurrentStress(t *testing.T) {
	t.Parallel()
	l := dataloader.New(func(_ context.Context, keys []string) (map[string]string, error) {
		out := make(map[string]string, len(keys))
		for _, k := range keys {
			out[k] = "v:" + k
		}
		return out, nil
	}, dataloader.WithWait(time.Microsecond), dataloader.WithMaxBatchSize(8))

	var wg sync.WaitGroup
	for w := range 8 {
		wg.Go(func() {
			for i := range 50 {
				key := string(rune('a' + (w+i)%16))
				switch i % 4 {
				case 0:
					if v, err := l.Load(t.Context(), key); err != nil || v != "v:"+key {
						t.Errorf("Load(%q) = %q, %v", key, v, err)
					}
				case 1:
					if _, err := l.LoadMany(t.Context(), []string{key, "x", "y"}); err != nil {
						t.Errorf("LoadMany: %v", err)
					}
				case 2:
					l.Prime(key, "v:"+key)
				default:
					l.Clear(key)
				}
			}
		})
	}
	wg.Wait()
}
