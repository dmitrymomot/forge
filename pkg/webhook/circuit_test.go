package webhook_test

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/pkg/webhook"
)

func TestCircuitBreaker_StateTransitions(t *testing.T) {
	t.Parallel()

	t.Run("Closed to Open", func(t *testing.T) {
		t.Parallel()

		cb := webhook.NewCircuitBreaker(2, 1, 100*time.Millisecond)

		assert.Equal(t, webhook.CircuitClosed, cb.State())
		assert.True(t, cb.Allow())

		cb.RecordFailure()
		assert.Equal(t, webhook.CircuitClosed, cb.State())
		assert.True(t, cb.Allow())

		cb.RecordFailure()
		assert.Equal(t, webhook.CircuitOpen, cb.State())
		assert.False(t, cb.Allow())
	})

	t.Run("Open to HalfOpen", func(t *testing.T) {
		t.Parallel()

		cb := webhook.NewCircuitBreaker(1, 1, 50*time.Millisecond)

		cb.RecordFailure()
		assert.Equal(t, webhook.CircuitOpen, cb.State())
		assert.False(t, cb.Allow())

		time.Sleep(60 * time.Millisecond)

		assert.True(t, cb.Allow())
		assert.Equal(t, webhook.CircuitHalfOpen, cb.State())
	})

	t.Run("HalfOpen to Closed", func(t *testing.T) {
		t.Parallel()

		cb := webhook.NewCircuitBreaker(1, 2, 50*time.Millisecond)

		cb.RecordFailure()
		time.Sleep(60 * time.Millisecond)

		assert.True(t, cb.Allow())
		assert.Equal(t, webhook.CircuitHalfOpen, cb.State())

		cb.RecordSuccess()
		assert.Equal(t, webhook.CircuitHalfOpen, cb.State())

		cb.RecordSuccess()
		assert.Equal(t, webhook.CircuitClosed, cb.State())
	})

	t.Run("HalfOpen to Open", func(t *testing.T) {
		t.Parallel()

		cb := webhook.NewCircuitBreaker(1, 2, 50*time.Millisecond)

		cb.RecordFailure()
		time.Sleep(60 * time.Millisecond)

		assert.True(t, cb.Allow())
		assert.Equal(t, webhook.CircuitHalfOpen, cb.State())

		cb.RecordFailure()
		assert.Equal(t, webhook.CircuitOpen, cb.State())
		assert.False(t, cb.Allow())
	})
}

func TestCircuitBreaker_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	cb := webhook.NewCircuitBreaker(10, 2, 100*time.Millisecond)

	const numGoroutines = 100
	const operationsPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := range numGoroutines {
		go func(id int) {
			defer wg.Done()

			for j := range operationsPerGoroutine {
				switch j % 4 {
				case 0:
					cb.Allow()
				case 1:
					cb.RecordSuccess()
				case 2:
					cb.RecordFailure()
				case 3:
					cb.State()
				}
			}
		}(i)
	}

	wg.Wait()

	state := cb.State()
	assert.Contains(t, []webhook.CircuitState{
		webhook.CircuitClosed,
		webhook.CircuitOpen,
		webhook.CircuitHalfOpen,
	}, state)

	stats := cb.Stats()
	assert.Contains(t, []string{"closed", "open", "half-open"}, stats.State)
	assert.GreaterOrEqual(t, stats.Failures, 0)
	assert.GreaterOrEqual(t, stats.SuccessCount, 0)
}

func TestCircuitBreaker_RecoveryTimeout(t *testing.T) {
	t.Parallel()

	t.Run("Respect Recovery Timeout", func(t *testing.T) {
		t.Parallel()

		cb := webhook.NewCircuitBreaker(1, 1, 100*time.Millisecond)

		cb.RecordFailure()
		assert.Equal(t, webhook.CircuitOpen, cb.State())
		assert.False(t, cb.Allow())

		time.Sleep(50 * time.Millisecond)
		assert.Equal(t, webhook.CircuitOpen, cb.State())
		assert.False(t, cb.Allow())

		time.Sleep(60 * time.Millisecond)
		assert.Equal(t, webhook.CircuitHalfOpen, cb.State())
		assert.True(t, cb.Allow())
	})

	t.Run("Multiple Failures Reset Timeout", func(t *testing.T) {
		t.Parallel()

		cb := webhook.NewCircuitBreaker(1, 1, 100*time.Millisecond)

		cb.RecordFailure()

		time.Sleep(80 * time.Millisecond)
		cb.RecordFailure()

		time.Sleep(30 * time.Millisecond)
		assert.Equal(t, webhook.CircuitOpen, cb.State())
		assert.False(t, cb.Allow())

		time.Sleep(80 * time.Millisecond)
		assert.Equal(t, webhook.CircuitHalfOpen, cb.State())
		assert.True(t, cb.Allow())
	})
}

func TestCircuitBreaker_HalfOpenSuccess(t *testing.T) {
	t.Parallel()

	t.Run("Single Success Required", func(t *testing.T) {
		t.Parallel()

		cb := webhook.NewCircuitBreaker(1, 1, 50*time.Millisecond)

		cb.RecordFailure()
		time.Sleep(60 * time.Millisecond)
		cb.Allow()

		assert.Equal(t, webhook.CircuitHalfOpen, cb.State())

		cb.RecordSuccess()
		assert.Equal(t, webhook.CircuitClosed, cb.State())
	})

	t.Run("Multiple Successes Required", func(t *testing.T) {
		t.Parallel()

		cb := webhook.NewCircuitBreaker(1, 3, 50*time.Millisecond)

		cb.RecordFailure()
		time.Sleep(60 * time.Millisecond)
		cb.Allow()

		assert.Equal(t, webhook.CircuitHalfOpen, cb.State())

		cb.RecordSuccess()
		assert.Equal(t, webhook.CircuitHalfOpen, cb.State())

		cb.RecordSuccess()
		assert.Equal(t, webhook.CircuitHalfOpen, cb.State())

		cb.RecordSuccess()
		assert.Equal(t, webhook.CircuitClosed, cb.State())
	})

	t.Run("Allow Multiple Requests in HalfOpen", func(t *testing.T) {
		t.Parallel()

		cb := webhook.NewCircuitBreaker(1, 2, 50*time.Millisecond)

		cb.RecordFailure()
		time.Sleep(60 * time.Millisecond)
		cb.Allow()

		assert.True(t, cb.Allow())
		assert.True(t, cb.Allow())
		assert.True(t, cb.Allow())
	})
}

func TestCircuitBreaker_ResetAndStats(t *testing.T) {
	t.Parallel()

	t.Run("Reset Functionality", func(t *testing.T) {
		t.Parallel()

		cb := webhook.NewCircuitBreaker(2, 1, 100*time.Millisecond)

		cb.RecordFailure()
		cb.RecordFailure()
		assert.Equal(t, webhook.CircuitOpen, cb.State())

		cb.Reset()
		assert.Equal(t, webhook.CircuitClosed, cb.State())
		assert.True(t, cb.Allow())

		stats := cb.Stats()
		assert.Equal(t, "closed", stats.State)
		assert.Equal(t, 0, stats.Failures)
		assert.Equal(t, 0, stats.SuccessCount)
		assert.True(t, stats.LastFailureTime.IsZero())
	})

	t.Run("Stats Accuracy", func(t *testing.T) {
		t.Parallel()

		cb := webhook.NewCircuitBreaker(3, 2, 100*time.Millisecond)

		stats := cb.Stats()
		assert.Equal(t, "closed", stats.State)
		assert.Equal(t, 0, stats.Failures)
		assert.Equal(t, 0, stats.SuccessCount)

		beforeFailure := time.Now()
		cb.RecordFailure()
		cb.RecordFailure()

		stats = cb.Stats()
		assert.Equal(t, "closed", stats.State)
		assert.Equal(t, 2, stats.Failures)
		assert.Equal(t, 0, stats.SuccessCount)
		assert.True(t, stats.LastFailureTime.After(beforeFailure))

		cb.RecordFailure()
		stats = cb.Stats()
		assert.Equal(t, "open", stats.State)
		assert.Equal(t, 3, stats.Failures)

		time.Sleep(110 * time.Millisecond)
		cb.Allow()
		cb.RecordSuccess()

		stats = cb.Stats()
		assert.Equal(t, "half-open", stats.State)
		assert.Equal(t, 3, stats.Failures)
		assert.Equal(t, 1, stats.SuccessCount)
	})

	t.Run("Success Resets Failures in Closed State", func(t *testing.T) {
		t.Parallel()

		cb := webhook.NewCircuitBreaker(5, 1, 100*time.Millisecond)

		cb.RecordFailure()
		cb.RecordFailure()

		stats := cb.Stats()
		assert.Equal(t, 2, stats.Failures)

		cb.RecordSuccess()
		stats = cb.Stats()
		assert.Equal(t, 0, stats.Failures)
		assert.Equal(t, "closed", stats.State)
	})
}

func TestCircuitBreaker_DefaultValues(t *testing.T) {
	t.Parallel()

	t.Run("Zero Values Get Defaults", func(t *testing.T) {
		t.Parallel()

		cb := webhook.NewCircuitBreaker(0, 0, 0)

		assert.Equal(t, webhook.CircuitClosed, cb.State())
		assert.True(t, cb.Allow())

		for range 4 {
			cb.RecordFailure()
			assert.Equal(t, webhook.CircuitClosed, cb.State())
		}

		cb.RecordFailure()
		assert.Equal(t, webhook.CircuitOpen, cb.State())
	})

	t.Run("Negative Values Get Defaults", func(t *testing.T) {
		t.Parallel()

		cb := webhook.NewCircuitBreaker(-1, -1, -1*time.Second)

		assert.Equal(t, webhook.CircuitClosed, cb.State())
		assert.True(t, cb.Allow())
	})

	t.Run("Partial Zero Values", func(t *testing.T) {
		t.Parallel()

		cb1 := webhook.NewCircuitBreaker(3, 0, 0)

		for range 2 {
			cb1.RecordFailure()
			assert.Equal(t, webhook.CircuitClosed, cb1.State())
		}

		cb1.RecordFailure()
		assert.Equal(t, webhook.CircuitOpen, cb1.State())

		cb2 := webhook.NewCircuitBreaker(0, 0, 50*time.Millisecond)
		cb2.RecordFailure()
		assert.Equal(t, webhook.CircuitClosed, cb2.State())
	})

	t.Run("String Representation", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, "closed", webhook.CircuitClosed.String())
		assert.Equal(t, "open", webhook.CircuitOpen.String())
		assert.Equal(t, "half-open", webhook.CircuitHalfOpen.String())

		invalidState := webhook.CircuitState(999)
		assert.Equal(t, "unknown", invalidState.String())
	})
}
