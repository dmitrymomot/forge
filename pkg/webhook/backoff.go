package webhook

import (
	"math"
	"math/rand"
	"time"
)

// BackoffStrategy defines the interface for calculating retry delays.
// Implementations should be safe for concurrent use.
type BackoffStrategy interface {
	// NextInterval returns the next backoff duration based on the attempt number.
	// Attempt starts at 1 for the first retry.
	NextInterval(attempt int) time.Duration
}

// ExponentialBackoff implements exponential backoff with jitter.
// Jitter prevents thundering herd when multiple clients retry simultaneously.
type ExponentialBackoff struct {
	InitialInterval time.Duration
	MaxInterval     time.Duration
	Multiplier      float64
	JitterFactor    float64
}

// NextInterval calculates exponential backoff with jitter to prevent coordinated retry storms.
// Formula: min(InitialInterval * (Multiplier ^ (attempt-1)) * (1 +/- JitterFactor), MaxInterval)
func (e ExponentialBackoff) NextInterval(attempt int) time.Duration {
	if attempt <= 0 {
		return 0
	}

	initial := e.InitialInterval
	if initial == 0 {
		initial = time.Second
	}

	maxInterval := e.MaxInterval
	if maxInterval == 0 {
		maxInterval = 30 * time.Second
	}

	multiplier := e.Multiplier
	if multiplier == 0 {
		multiplier = 2
	}

	// Calculate exponential growth: initial * (multiplier ^ (attempt-1))
	interval := float64(initial) * math.Pow(multiplier, float64(attempt-1))

	// Apply jitter to spread retry times and prevent thundering herd
	if e.JitterFactor > 0 {
		randomJitter := (rand.Float64()*2 - 1) * e.JitterFactor
		interval = interval * (1 + randomJitter)
	}

	// Respect maximum interval to prevent excessively long delays
	if interval > float64(maxInterval) {
		interval = float64(maxInterval)
	}

	return time.Duration(interval)
}

// LinearBackoff implements simple linear backoff.
// Provides predictable retry intervals that increase linearly.
type LinearBackoff struct {
	Interval    time.Duration
	MaxInterval time.Duration
}

// NextInterval returns linearly increasing delays.
// Formula: min(Interval * attempt, MaxInterval)
func (l LinearBackoff) NextInterval(attempt int) time.Duration {
	if attempt <= 0 {
		return 0
	}

	interval := l.Interval
	if interval == 0 {
		interval = time.Second
	}

	maxInterval := l.MaxInterval
	if maxInterval == 0 {
		maxInterval = 30 * time.Second
	}

	delay := interval * time.Duration(attempt)
	return min(delay, maxInterval)
}

// FixedBackoff implements a constant delay between retries.
type FixedBackoff struct {
	Interval time.Duration
}

// NextInterval always returns the same interval regardless of attempt number.
func (f FixedBackoff) NextInterval(attempt int) time.Duration {
	if attempt <= 0 {
		return 0
	}
	return f.Interval
}

// DefaultBackoffStrategy returns production-ready exponential backoff.
func DefaultBackoffStrategy() BackoffStrategy {
	return ExponentialBackoff{
		InitialInterval: time.Second,
		MaxInterval:     30 * time.Second,
		Multiplier:      2,
		JitterFactor:    0.1,
	}
}
