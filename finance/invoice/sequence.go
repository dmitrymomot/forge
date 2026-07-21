package invoice

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
)

// NumberingMode declares the jurisdictional numbering requirement of a
// series. The mode is a construction-time contract that changes when numbers
// may be drawn, not just documentation.
type NumberingMode int

const (
	// Gapless is the strict transactional mode: numbers are drawn only
	// inside Issue, never pre-assigned and never via Sequence.Next, and the
	// store's counter increment is expected to ride the same transaction
	// that persists the issued document (the store implementation reads the
	// transaction from ctx), so a failed issuance rolls the number back and
	// the series stays gapless. Required by many EU jurisdictions.
	Gapless NumberingMode = iota
	// WithGaps is the monotonic mode: numbers may be pre-drawn via
	// Sequence.Next (number previews, async issuance) or pre-assigned on the
	// draft, and a failed issuance burns the number. Sufficient wherever
	// only uniqueness and monotonicity are required.
	WithGaps
)

// SequenceStore is the persistence seam for per-series counters.
// Implementations backing a Gapless sequence must perform the increment
// inside the caller's persistence transaction carried in ctx.
type SequenceStore interface {
	// Next atomically increments the counter for series and returns the new
	// value, starting at 1 for an unseen series.
	Next(ctx context.Context, series string) (int64, error)
}

// Sequence draws document numbers from a per-series counter behind a
// SequenceStore, formats them for display, and optionally namespaces series
// per tenant via a scope hook.
type Sequence struct {
	store SequenceStore
	cfg   sequenceConfig
}

// NewSequence builds a Sequence over store. The default mode is Gapless (the
// strict jurisdictions' requirement — opting into gaps is the explicit
// choice), the default format is "SERIES-000042".
func NewSequence(store SequenceStore, opts ...Option) (*Sequence, error) {
	if store == nil {
		return nil, errors.New("invoice: nil sequence store")
	}
	cfg := sequenceConfig{mode: Gapless, format: defaultFormat}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.mode != Gapless && cfg.mode != WithGaps {
		return nil, fmt.Errorf("invoice: unknown numbering mode %d", cfg.mode)
	}
	if cfg.format == nil {
		return nil, errors.New("invoice: nil number format")
	}
	return &Sequence{store: store, cfg: cfg}, nil
}

// Mode returns the sequence's declared numbering mode.
func (s *Sequence) Mode() NumberingMode { return s.cfg.mode }

// Next pre-draws the next formatted number for series. It is only available
// in WithGaps mode; a Gapless sequence returns ErrGapless because gapless
// numbers exist only from the moment a document is issued.
func (s *Sequence) Next(ctx context.Context, series string) (string, error) {
	if s.cfg.mode == Gapless {
		return "", ErrGapless
	}
	return s.next(ctx, series)
}

// next draws and formats the next number, applying the tenant scope to the
// stored counter key while formatting with the caller-visible series.
func (s *Sequence) next(ctx context.Context, series string) (string, error) {
	key := series
	if s.cfg.scope != nil {
		scope, err := s.cfg.scope(ctx)
		if err != nil {
			return "", fmt.Errorf("%w: %w", ErrScope, err)
		}
		if scope == "" {
			return "", ErrScope
		}
		// Length-prefix the scope so (scope, series) pairs can never
		// collide, whatever characters they contain.
		key = strconv.Itoa(len(scope)) + ":" + scope + ":" + series
	}
	n, err := s.store.Next(ctx, key)
	if err != nil {
		return "", err
	}
	return s.cfg.format(series, n), nil
}

func defaultFormat(series string, n int64) string {
	return fmt.Sprintf("%s-%06d", series, n)
}

// MemorySequenceStore is the in-memory SequenceStore for tests and
// development. It cannot participate in a caller's transaction, so a Gapless
// sequence over it is gapless only as long as every drawn number is
// persisted; production gapless series need a transactional store.
type MemorySequenceStore struct {
	counters map[string]int64
	mu       sync.Mutex
}

// NewMemorySequenceStore returns an empty in-memory counter store.
func NewMemorySequenceStore() *MemorySequenceStore {
	return &MemorySequenceStore{counters: make(map[string]int64)}
}

// Next implements SequenceStore.
func (m *MemorySequenceStore) Next(ctx context.Context, series string) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.counters[series]++
	return m.counters[series], nil
}
