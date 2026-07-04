// Package circuitbreaker fails fast against a failing dependency using a
// closed/open/half-open state machine.
//
// # Usage
//
//	cb := circuitbreaker.New(circuitbreaker.WithFailureThreshold(5))
//	err := cb.Do(ctx, func(ctx context.Context) error { return call(ctx) })
//	if errors.Is(err, circuitbreaker.ErrOpen) {
//	    // dependency is being given time to recover
//	}
package circuitbreaker
