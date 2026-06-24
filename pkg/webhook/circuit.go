package webhook

import (
	"sync"
	"time"
)

// CircuitState represents the current state of the circuit breaker.
type CircuitState int

const (
	// CircuitClosed allows requests to pass through.
	CircuitClosed CircuitState = iota
	// CircuitOpen blocks all requests.
	CircuitOpen
	// CircuitHalfOpen allows one request to test if the service has recovered.
	CircuitHalfOpen
)

// String returns the string representation of the circuit state.
func (s CircuitState) String() string {
	switch s {
	case CircuitClosed:
		return "closed"
	case CircuitOpen:
		return "open"
	case CircuitHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// CircuitBreaker implements a simple circuit breaker pattern to prevent
// hammering failed endpoints. Safe for concurrent use.
type CircuitBreaker struct {
	lastFailureTime time.Time

	failureThreshold int
	recoveryTimeout  time.Duration
	successThreshold int

	state        CircuitState
	failures     int
	successCount int

	// halfOpenProbeInFlight tracks whether a probe request is currently being
	// tested in the half-open state. Only one probe is allowed at a time so a
	// recovering endpoint is not hammered by concurrent traffic.
	halfOpenProbeInFlight bool

	mu sync.RWMutex
}

// NewCircuitBreaker creates a circuit breaker with the given configuration.
// Default values provide reasonable protection for most webhook scenarios.
func NewCircuitBreaker(failureThreshold, successThreshold int, recoveryTimeout time.Duration) *CircuitBreaker {
	if failureThreshold <= 0 {
		failureThreshold = 5
	}
	if successThreshold <= 0 {
		successThreshold = 2
	}
	if recoveryTimeout <= 0 {
		recoveryTimeout = 30 * time.Second
	}

	return &CircuitBreaker{
		failureThreshold: failureThreshold,
		recoveryTimeout:  recoveryTimeout,
		successThreshold: successThreshold,
		state:            CircuitClosed,
	}
}

// Allow checks if a request should be allowed through the circuit breaker.
func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case CircuitClosed:
		return true

	case CircuitOpen:
		if time.Since(cb.lastFailureTime) > cb.recoveryTimeout {
			cb.state = CircuitHalfOpen
			cb.successCount = 0
			// Begin the single permitted probe for this recovery window.
			cb.halfOpenProbeInFlight = true
			return true
		}
		return false

	case CircuitHalfOpen:
		// Only one probe is allowed in flight at a time. Concurrent callers are
		// rejected until the outstanding probe records a success or failure.
		if cb.halfOpenProbeInFlight {
			return false
		}
		cb.halfOpenProbeInFlight = true
		return true

	default:
		return false
	}
}

// RecordSuccess records a successful request and may close the circuit.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case CircuitClosed:
		cb.failures = 0

	case CircuitHalfOpen:
		// The probe resolved; release the slot so the next probe can run if more
		// successes are still required to fully close the circuit.
		cb.halfOpenProbeInFlight = false
		cb.successCount++
		if cb.successCount >= cb.successThreshold {
			cb.state = CircuitClosed
			cb.failures = 0
			cb.successCount = 0
		}
	}
}

// RecordFailure records a failed request and may open the circuit.
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.lastFailureTime = time.Now()

	switch cb.state {
	case CircuitClosed:
		cb.failures++
		if cb.failures >= cb.failureThreshold {
			cb.state = CircuitOpen
		}

	case CircuitHalfOpen:
		// The probe failed; reopen the circuit and release the probe slot for
		// the next recovery window.
		cb.state = CircuitOpen
		cb.failures = cb.failureThreshold
		cb.successCount = 0
		cb.halfOpenProbeInFlight = false
	}
}

// State returns the current state, accounting for automatic transitions.
func (cb *CircuitBreaker) State() CircuitState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	if cb.state == CircuitOpen && time.Since(cb.lastFailureTime) > cb.recoveryTimeout {
		return CircuitHalfOpen
	}

	return cb.state
}

// Reset resets the circuit breaker to closed state.
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.state = CircuitClosed
	cb.failures = 0
	cb.successCount = 0
	cb.lastFailureTime = time.Time{}
	cb.halfOpenProbeInFlight = false
}

// CircuitStats provides visibility into circuit breaker state for monitoring.
type CircuitStats struct {
	LastFailureTime time.Time
	State           string
	Failures        int
	SuccessCount    int
}

// Stats returns the current statistics of the circuit breaker.
func (cb *CircuitBreaker) Stats() CircuitStats {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	return CircuitStats{
		State:           cb.state.String(),
		Failures:        cb.failures,
		SuccessCount:    cb.successCount,
		LastFailureTime: cb.lastFailureTime,
	}
}
