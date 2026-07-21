package approval

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"time"

	"github.com/dmitrymomot/forge/core/id"
)

// now returns the manager's clock time, normalized to what a Postgres
// timestamptz can hold. Untruncated nanoseconds do not survive the
// round-trip, so a stored request would not equal the one just returned.
func (m *Manager) now() time.Time {
	return m.cfg.clk.Now().UTC().Truncate(time.Microsecond)
}

// Submit records a new approval request for kind k carrying payload.
//
// The returned request is Pending: it becomes actionable to checkers
// immediately and expires after the kind's policy TTL. Submitting does not
// authorize anything by itself — the maker's own decision never counts.
func Submit[T any](ctx context.Context, m *Manager, k Kind[T], payload T, p SubmitParams) (Request, error) {
	pol, ok := m.policyFor(k.Name())
	if !ok {
		return Request{}, ErrUnknownKind
	}
	if p.Requester == "" {
		return Request{}, ErrRequesterRequired
	}
	tenant, err := m.scoped(ctx, p.Tenant)
	if err != nil {
		return Request{}, err
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return Request{}, fmt.Errorf("approval: marshal payload: %w", err)
	}

	now := m.now()
	r := Request{
		ID:        id.NewUUID(),
		Kind:      k.Name(),
		Tenant:    tenant,
		Requester: p.Requester,
		Reason:    p.Reason,
		Status:    Pending,
		Version:   1,
		Payload:   raw,
		Meta:      maps.Clone(p.Meta),
		Decisions: make([]Decision, 0, pol.Quorum),
		CreatedAt: now,
	}
	if pol.TTL > 0 {
		r.ExpiresAt = now.Add(pol.TTL)
	}
	if err := m.store.Create(ctx, r); err != nil {
		return Request{}, err
	}
	return r, m.audit(ctx, r, actionSubmit, p.Requester, outcomeSuccess, p.Reason)
}

// PayloadOf decodes r's payload as T. It returns ErrKindMismatch when k is
// not the kind r was submitted under — decoding another action's payload
// into T would silently produce a zero-valued struct.
func PayloadOf[T any](k Kind[T], r Request) (T, error) {
	var out T
	if r.Kind != k.Name() {
		return out, fmt.Errorf("%w: request is %q, kind is %q", ErrKindMismatch, r.Kind, k.Name())
	}
	if err := json.Unmarshal(r.Payload, &out); err != nil {
		return out, fmt.Errorf("approval: unmarshal payload: %w", err)
	}
	return out, nil
}
