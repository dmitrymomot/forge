package approval

import (
	"context"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/core/id"
)

// Manager records approval requests, collects decisions on them, and hands
// approved requests to exactly one executor. Safe for concurrent use.
type Manager struct {
	store Store
	cfg   config
}

// New builds a Manager over store. Register at least one kind with
// WithKind.
//
// It panics on a nil store, on a Manager with no registered kinds, and on
// any invalid policy — wiring bugs caught at startup rather than on the
// first payout, matching apikey.New's nil-store panic.
func New(store Store, opts ...Option) *Manager {
	if store == nil {
		panic("approval: nil store")
	}
	cfg := config{
		clk:        clock.System(),
		kinds:      make(map[string]Policy),
		maxRetries: 3,
	}
	for _, o := range opts {
		o(&cfg)
	}
	if len(cfg.kinds) == 0 {
		panic("approval: no kinds registered; every submission would fail with ErrUnknownKind")
	}
	return &Manager{store: store, cfg: cfg}
}

// policyFor returns the policy registered for kind. The registry is
// immutable after New, so this read needs no lock.
//
//nolint:unused // consumed by Submit, added in Task 4 of this package's build.
func (m *Manager) policyFor(kind string) (Policy, bool) {
	p, ok := m.cfg.kinds[kind]
	return p, ok
}

// Get loads one request, with expiry applied: a Pending or Approved
// request past its ExpiresAt reports Status Expired even though the stored
// row still carries its last written status.
func (m *Manager) Get(ctx context.Context, reqID id.UUID) (Request, error) {
	r, err := m.store.Get(ctx, reqID)
	if err != nil {
		return Request{}, err
	}
	m.applyExpiry(&r)
	return r, nil
}

// applyExpiry derives the effective status of r. Expiry is never written:
// the stored row keeps its last written status and the effective status is
// computed on every read, so no sweeper is needed for correctness. Only
// Pending and Approved expire — an Executing request is governed by its
// claim lease, not by TTL.
func (m *Manager) applyExpiry(r *Request) {
	if r.ExpiresAt.IsZero() {
		return
	}
	if r.Status != Pending && r.Status != Approved {
		return
	}
	if m.cfg.clk.Now().Before(r.ExpiresAt) {
		return
	}
	r.Status = Expired
}
