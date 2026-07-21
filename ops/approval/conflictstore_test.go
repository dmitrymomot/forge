package approval_test

import (
	"context"

	"github.com/dmitrymomot/forge/ops/approval"
)

// conflictStore wraps a Store and returns ErrConflict from Update for the
// first n calls, then delegates. It is a deterministic stand-in for a real
// CAS race, letting mutate's retry loop be tested without relying on
// goroutine scheduling.
//
// before, if set, runs exactly once — right when the injected calls run
// out — letting a test commit a "winning" write to the wrapped Store
// between the losing attempt and the retry that must observe it.
type conflictStore struct {
	approval.Store
	before func()
	calls  int
	left   int
}

func (s *conflictStore) Update(ctx context.Context, r approval.Request, expect int64) error {
	s.calls++
	if s.left <= 0 {
		return s.Store.Update(ctx, r, expect)
	}
	s.left--
	if s.before != nil {
		before := s.before
		s.before = nil
		before()
	}
	return approval.ErrConflict
}
