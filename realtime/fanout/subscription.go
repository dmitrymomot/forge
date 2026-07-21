package fanout

import (
	"sync"
	"sync/atomic"
)

// Message is one delivery. Payload is shared across every subscriber of the
// topic — do not mutate it. ID is monotonically increasing per hub instance
// and is the resume cursor for WithResumeAfter; it is not stable across
// instances or restarts.
type Message struct {
	Topic   string
	Payload []byte
	ID      uint64
}

// OverflowPolicy decides what happens when a message arrives and the
// subscription's buffer is full.
type OverflowPolicy uint8

const (
	// DropOldest evicts the oldest buffered message to make room (default):
	// a stalled consumer resumes with the newest messages.
	DropOldest OverflowPolicy = iota
	// DropNewest discards the incoming message, preserving the buffered
	// backlog.
	DropNewest
	// CloseSlow tears the subscription down: its channel closes and Err
	// reports ErrSlowConsumer. The client must resubscribe (typically with
	// WithResumeAfter) to catch up.
	CloseSlow
)

// Subscription is one subscriber's bounded delivery stream. Receive from C;
// Close when done. Safe for concurrent use, but C is a single stream — one
// consumer per subscription.
type Subscription struct {
	err     error
	hub     *Hub
	ch      chan Message
	states  []*topicState
	keys    []string
	dropped atomic.Uint64
	mu      sync.Mutex
	policy  OverflowPolicy
	closed  bool
}

// C returns the delivery channel. It closes when Close is called or the hub
// tears the subscription down; check Err after it closes to learn why.
func (s *Subscription) C() <-chan Message { return s.ch }

// Dropped reports how many messages this subscription lost to its overflow
// policy (including replayed backlog that did not fit the buffer).
func (s *Subscription) Dropped() uint64 { return s.dropped.Load() }

// Err reports why the hub closed the subscription: ErrSlowConsumer under the
// CloseSlow policy, ErrClosed on hub Close, nil while open or after the
// subscriber's own Close.
func (s *Subscription) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// Close unsubscribes and closes C. Idempotent and safe concurrently with
// publishes.
func (s *Subscription) Close() {
	if s.closeWith(nil) {
		s.hub.removeSub(s)
	}
}

// closeWith marks the subscription closed with the given reason and closes
// the channel. It reports whether this call performed the close.
func (s *Subscription) closeWith(err error) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	s.closed = true
	s.err = err
	close(s.ch)
	return true
}

// send delivers m without ever blocking, applying the overflow policy on a
// full buffer. It reports whether the subscription is (now) closed and
// should be removed from topic registries. Callers hold the topicState
// mutex; lock order is always topicState → Subscription.
func (s *Subscription) send(m Message) (remove bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return true
	}
	select {
	case s.ch <- m:
		return false
	default:
	}
	switch s.policy {
	case DropNewest:
		s.dropped.Add(1)
	case CloseSlow:
		s.closed = true
		s.err = ErrSlowConsumer
		close(s.ch)
		return true
	default: // DropOldest
		select {
		case <-s.ch:
			s.dropped.Add(1)
		default:
			// The consumer drained the buffer between the full check and the
			// eviction; there is room now.
		}
		select {
		case s.ch <- m:
		default:
			// Unreachable: sends are serialized by s.mu and the consumer only
			// drains, but never block regardless.
			s.dropped.Add(1)
		}
	}
	return false
}
