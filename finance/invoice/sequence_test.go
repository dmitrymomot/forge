package invoice_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/finance/invoice"
)

func TestNewSequence(t *testing.T) {
	t.Parallel()

	t.Run("nil store", func(t *testing.T) {
		t.Parallel()
		_, err := invoice.NewSequence(nil)
		assert.Error(t, err)
	})
	t.Run("unknown mode", func(t *testing.T) {
		t.Parallel()
		_, err := invoice.NewSequence(invoice.NewMemorySequenceStore(), invoice.WithMode(invoice.NumberingMode(7)))
		assert.Error(t, err)
	})
	t.Run("nil format", func(t *testing.T) {
		t.Parallel()
		_, err := invoice.NewSequence(invoice.NewMemorySequenceStore(), invoice.WithFormat(nil))
		assert.Error(t, err)
	})
	t.Run("default mode is gapless", func(t *testing.T) {
		t.Parallel()
		seq, err := invoice.NewSequence(invoice.NewMemorySequenceStore())
		require.NoError(t, err)
		assert.Equal(t, invoice.Gapless, seq.Mode())
	})
}

func TestSequence_Next(t *testing.T) {
	t.Parallel()

	t.Run("gapless refuses pre-draw", func(t *testing.T) {
		t.Parallel()
		seq := newSequence(t)
		_, err := seq.Next(t.Context(), "INV-2026")
		assert.ErrorIs(t, err, invoice.ErrGapless)
	})
	t.Run("series count independently", func(t *testing.T) {
		t.Parallel()
		seq := newSequence(t, invoice.WithMode(invoice.WithGaps))
		for _, want := range []string{"INV-2026-000001", "INV-2026-000002"} {
			n, err := seq.Next(t.Context(), "INV-2026")
			require.NoError(t, err)
			assert.Equal(t, want, n)
		}
		n, err := seq.Next(t.Context(), "CN-2026")
		require.NoError(t, err)
		assert.Equal(t, "CN-2026-000001", n)
	})
	t.Run("custom format", func(t *testing.T) {
		t.Parallel()
		seq := newSequence(t,
			invoice.WithMode(invoice.WithGaps),
			invoice.WithFormat(func(series string, n int64) string {
				return fmt.Sprintf("%s/%03d", series, n)
			}),
		)
		n, err := seq.Next(t.Context(), "INV")
		require.NoError(t, err)
		assert.Equal(t, "INV/001", n)
	})
	t.Run("cancelled context", func(t *testing.T) {
		t.Parallel()
		seq := newSequence(t, invoice.WithMode(invoice.WithGaps))
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		_, err := seq.Next(ctx, "INV")
		assert.ErrorIs(t, err, context.Canceled)
	})
}

func TestSequence_ConcurrentDrawsAreUnique(t *testing.T) {
	t.Parallel()

	seq := newSequence(t, invoice.WithMode(invoice.WithGaps))
	const workers, draws = 8, 50

	var mu sync.Mutex
	seen := make(map[string]bool, workers*draws)
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for range draws {
				n, err := seq.Next(context.Background(), "INV")
				if err != nil {
					t.Error(err)
					return
				}
				mu.Lock()
				if seen[n] {
					t.Errorf("duplicate number %s", n)
				}
				seen[n] = true
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	assert.Len(t, seen, workers*draws)
}

type failingScopeErr struct{}

func (failingScopeErr) Error() string { return "no tenant in context" }

func TestSequence_Tenancy(t *testing.T) {
	t.Parallel()

	type tenantKey struct{}
	scopeFromCtx := func(ctx context.Context) (string, error) {
		s, _ := ctx.Value(tenantKey{}).(string)
		if s == "" {
			return "", failingScopeErr{}
		}
		return s, nil
	}
	tenantCtx := func(id string) context.Context {
		return context.WithValue(context.Background(), tenantKey{}, id)
	}

	t.Run("tenants count independently with clean numbers", func(t *testing.T) {
		t.Parallel()
		seq := newSequence(t, invoice.WithMode(invoice.WithGaps), invoice.WithScope(scopeFromCtx))

		a1, err := seq.Next(tenantCtx("tenant-a"), "INV")
		require.NoError(t, err)
		a2, err := seq.Next(tenantCtx("tenant-a"), "INV")
		require.NoError(t, err)
		b1, err := seq.Next(tenantCtx("tenant-b"), "INV")
		require.NoError(t, err)

		assert.Equal(t, "INV-000001", a1)
		assert.Equal(t, "INV-000002", a2)
		assert.Equal(t, "INV-000001", b1, "tenant B starts its own count; numbers never leak the scope")
	})
	t.Run("missing scope fails closed", func(t *testing.T) {
		t.Parallel()
		seq := newSequence(t, invoice.WithMode(invoice.WithGaps), invoice.WithScope(scopeFromCtx))
		_, err := seq.Next(context.Background(), "INV")
		assert.ErrorIs(t, err, invoice.ErrScope)
		var cause failingScopeErr
		assert.True(t, errors.As(err, &cause), "hook error is preserved in the chain")
	})
	t.Run("empty scope fails closed", func(t *testing.T) {
		t.Parallel()
		seq := newSequence(t, invoice.WithMode(invoice.WithGaps),
			invoice.WithScope(func(context.Context) (string, error) { return "", nil }))
		_, err := seq.Next(context.Background(), "INV")
		assert.ErrorIs(t, err, invoice.ErrScope)
	})
	t.Run("scope and series can never collide", func(t *testing.T) {
		t.Parallel()
		// ("a", "b:c") vs ("a:b", "c"): naive concatenation would merge
		// these counters; the length-prefixed key must not.
		seq := newSequence(t, invoice.WithMode(invoice.WithGaps), invoice.WithScope(scopeFromCtx))
		n1, err := seq.Next(tenantCtx("a"), "b:c")
		require.NoError(t, err)
		n2, err := seq.Next(tenantCtx("a:b"), "c")
		require.NoError(t, err)
		assert.Equal(t, "b:c-000001", n1)
		assert.Equal(t, "c-000001", n2, "second pair must start at 1, not continue the first")
	})
	t.Run("scoped issue fails closed too", func(t *testing.T) {
		t.Parallel()
		seq := newSequence(t, invoice.WithScope(scopeFromCtx))
		inv := draft(t)
		err := inv.Issue(context.Background(), seq, issueTime)
		assert.ErrorIs(t, err, invoice.ErrScope)
		assert.Equal(t, invoice.StatusDraft, inv.Status)

		require.NoError(t, inv.Issue(tenantCtx("tenant-a"), seq, issueTime))
		assert.Equal(t, "INV-2026-000001", inv.Number)
	})
}
