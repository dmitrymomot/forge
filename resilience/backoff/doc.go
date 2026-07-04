// Package backoff computes retry delays as pure, stateless strategies.
//
// # Usage
//
//	b := backoff.Exponential(100*time.Millisecond, 10*time.Second, backoff.WithJitter(0.5))
//	for attempt := 1; attempt <= 5; attempt++ {
//	    time.Sleep(b.Next(attempt))
//	}
package backoff
