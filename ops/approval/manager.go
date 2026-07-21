package approval

import (
	"context"
	"errors"
	"slices"

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
func (m *Manager) policyFor(kind string) (Policy, bool) {
	p, ok := m.cfg.kinds[kind]
	return p, ok
}

// scoped resolves the tenant an operation is confined to. Tenancy lands in
// Task 11; until then it passes the requested tenant through.
func (m *Manager) scoped(_ context.Context, requested string) (string, error) {
	return requested, nil
}

// audit records a state change. The auditlog seam lands in Task 10; until
// then it is a no-op.
func (m *Manager) audit(_ context.Context, _ Request, _, _, _, _ string) error {
	return nil
}

// Audit action names and outcomes.
const (
	actionSubmit   = "approval.submit"
	outcomeSuccess = "success"
)

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

// List returns requests matching f, newest first, with expiry applied to
// each record.
//
// Filter.Statuses matches the STORED status, so a record that has expired
// out of the requested set is dropped after the fact: List returns UP TO
// Limit records, and a Statuses filter may yield fewer than the store
// matched. Query with no Statuses to see expired records with their derived
// status.
func (m *Manager) List(ctx context.Context, f Filter) ([]Request, error) {
	tenant, err := m.scoped(ctx, f.Tenant)
	if err != nil {
		return nil, err
	}
	f.Tenant = tenant

	out, err := m.store.List(ctx, f)
	if err != nil {
		return nil, err
	}
	kept := out[:0]
	for i := range out {
		m.applyExpiry(&out[i])
		if len(f.Statuses) > 0 && !slices.Contains(f.Statuses, out[i].Status) {
			continue
		}
		kept = append(kept, out[i])
	}
	return kept, nil
}

// mutate is the one concurrency primitive every transition rides: read the
// current record, derive its effective status, let fn validate and apply
// the transition, then compare-and-swap it back.
//
// fn is re-run from a FRESH read on every conflict retry — never from the
// previous attempt's copy. That re-validation is load-bearing: without it a
// vote that lost a race could be applied a second time on top of the
// winner's write, pushing a request past quorum with one approver counted
// twice.
//
//nolint:unused // consumed by Approve/Reject/Cancel/Claim/Complete/Fail/Release, added in Tasks 6-8 of this package's build.
func (m *Manager) mutate(ctx context.Context, reqID id.UUID, fn func(r *Request) error) (Request, error) {
	tenant, err := m.scoped(ctx, "")
	if err != nil {
		return Request{}, err
	}
	for attempt := 0; ; attempt++ {
		r, err := m.store.Get(ctx, reqID)
		if err != nil {
			return Request{}, err
		}
		// Report a foreign-tenant request as missing rather than forbidden,
		// so cross-tenant existence cannot be probed.
		if tenant != "" && r.Tenant != tenant {
			return Request{}, ErrNotFound
		}
		expect := r.Version
		m.applyExpiry(&r)

		if err := fn(&r); err != nil {
			return Request{}, err
		}

		err = m.store.Update(ctx, r, expect)
		switch {
		case err == nil:
			r.Version = expect + 1
			return r, nil
		case !errors.Is(err, ErrConflict):
			return Request{}, err
		case attempt >= m.cfg.maxRetries:
			return Request{}, ErrConflict
		}
	}
}

// statusErr maps a non-Pending status to the sentinel that explains it.
//
//nolint:unused // consumed by Tasks 6-8's transitions.
func statusErr(s Status) error {
	if s == Expired {
		return ErrExpired
	}
	return ErrNotPending
}
