// Package approvaltest is the executable contract for approval.Store
// implementations. Every driver's test suite must call Run; the in-memory
// store is the reference implementation.
//
// Fixtures are namespaced per subtest with a fresh UUID because a real
// backend's table outlives the test process: deterministic kinds and
// tenants would collide with a previous run's rows and inflate List counts.
package approvaltest

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/id"
	"github.com/dmitrymomot/forge/ops/approval"
)

// ns is a per-subtest namespace keeping fixtures disjoint from every other
// run against the same backend.
type ns struct{ kind, tenant string }

func newNS() ns {
	u := id.NewUUID().String()
	return ns{kind: "kind-" + u, tenant: "tenant-" + u}
}

func (n ns) request(requester string, status approval.Status) approval.Request {
	now := time.Now().UTC().Truncate(time.Microsecond)
	return approval.Request{
		ID:        id.NewUUID(),
		Kind:      n.kind,
		Tenant:    n.tenant,
		Requester: requester,
		Status:    status,
		Version:   1,
		Payload:   json.RawMessage(`{"amount":100}`),
		CreatedAt: now,
	}
}

// Run executes the Store conformance suite. factory must return a fresh or
// namespace-isolated store each call. The Manager's CAS retry loop depends
// on these exact semantics, so an implementation that diverges here breaks
// dual control.
func Run(t *testing.T, factory func(t *testing.T) approval.Store) {
	t.Helper()

	t.Run("CreateGetRoundTrip", func(t *testing.T) {
		s, n, ctx := factory(t), newNS(), context.Background()
		r := n.request("alice", approval.Pending)
		require.NoError(t, s.Create(ctx, r))

		got, err := s.Get(ctx, r.ID)
		require.NoError(t, err)
		assert.Equal(t, r.ID, got.ID)
		assert.Equal(t, r.Kind, got.Kind)
		assert.Equal(t, r.Requester, got.Requester)
		assert.Equal(t, approval.Pending, got.Status)
		assert.Equal(t, int64(1), got.Version)
		assert.JSONEq(t, string(r.Payload), string(got.Payload))
		assert.True(t, r.CreatedAt.Equal(got.CreatedAt), "timestamps survive the round-trip")
	})

	t.Run("CreateRejectsDuplicateID", func(t *testing.T) {
		s, n, ctx := factory(t), newNS(), context.Background()
		r := n.request("alice", approval.Pending)
		require.NoError(t, s.Create(ctx, r))
		assert.ErrorIs(t, s.Create(ctx, r), approval.ErrDuplicate)
	})

	t.Run("GetReportsNotFound", func(t *testing.T) {
		s := factory(t)
		_, err := s.Get(context.Background(), id.NewUUID())
		assert.ErrorIs(t, err, approval.ErrNotFound)
	})

	t.Run("UpdateAdvancesVersion", func(t *testing.T) {
		s, n, ctx := factory(t), newNS(), context.Background()
		r := n.request("alice", approval.Pending)
		require.NoError(t, s.Create(ctx, r))

		r.Status = approval.Approved
		require.NoError(t, s.Update(ctx, r, 1))

		got, err := s.Get(ctx, r.ID)
		require.NoError(t, err)
		assert.Equal(t, approval.Approved, got.Status)
		assert.Equal(t, int64(2), got.Version, "store sets version to expect+1")
	})

	t.Run("UpdateConflictsOnStaleVersion", func(t *testing.T) {
		s, n, ctx := factory(t), newNS(), context.Background()
		r := n.request("alice", approval.Pending)
		require.NoError(t, s.Create(ctx, r))
		require.NoError(t, s.Update(ctx, r, 1))

		// A second writer still believing the version is 1 must lose.
		assert.ErrorIs(t, s.Update(ctx, r, 1), approval.ErrConflict)
	})

	t.Run("UpdateReportsNotFound", func(t *testing.T) {
		s, n := factory(t), newNS()
		assert.ErrorIs(t, s.Update(context.Background(), n.request("alice", approval.Pending), 1),
			approval.ErrNotFound)
	})

	t.Run("StoredStateIsNotAliased", func(t *testing.T) {
		s, n, ctx := factory(t), newNS(), context.Background()
		r := n.request("alice", approval.Pending)
		r.Decisions = []approval.Decision{{
			At: time.Now().UTC().Truncate(time.Microsecond), Approver: "bob", Vote: approval.VoteApprove,
		}}
		require.NoError(t, s.Create(ctx, r))

		r.Decisions[0].Approver = "mallory" // mutate the caller's slice
		got, err := s.Get(ctx, r.ID)
		require.NoError(t, err)
		require.Len(t, got.Decisions, 1)
		assert.Equal(t, "bob", got.Decisions[0].Approver,
			"a caller must not be able to rewrite a persisted vote")
	})

	t.Run("DecisionsRoundTrip", func(t *testing.T) {
		s, n, ctx := factory(t), newNS(), context.Background()
		at := time.Now().UTC().Truncate(time.Microsecond)
		r := n.request("alice", approval.Pending)
		r.Decisions = []approval.Decision{
			{At: at, Approver: "bob", Reason: "checked", Vote: approval.VoteApprove},
			{At: at, Approver: "carol", Vote: approval.VoteReject},
		}
		require.NoError(t, s.Create(ctx, r))

		got, err := s.Get(ctx, r.ID)
		require.NoError(t, err)
		require.Len(t, got.Decisions, 2)
		assert.Equal(t, "bob", got.Decisions[0].Approver)
		assert.Equal(t, "checked", got.Decisions[0].Reason)
		assert.Equal(t, approval.VoteApprove, got.Decisions[0].Vote)
		assert.Equal(t, approval.VoteReject, got.Decisions[1].Vote)
		assert.True(t, at.Equal(got.Decisions[0].At))
	})

	t.Run("ListFilters", func(t *testing.T) {
		s, n, ctx := factory(t), newNS(), context.Background()
		pending := n.request("alice", approval.Pending)
		approved := n.request("alice", approval.Approved)
		otherRequester := n.request("carol", approval.Pending)
		for _, r := range []approval.Request{pending, approved, otherRequester} {
			require.NoError(t, s.Create(ctx, r))
		}
		// A same-kind row in a different tenant must never leak in.
		foreign := n.request("alice", approval.Pending)
		foreign.Tenant = "tenant-" + id.NewUUID().String()
		require.NoError(t, s.Create(ctx, foreign))

		got, err := s.List(ctx, approval.Filter{
			Statuses:  []approval.Status{approval.Pending},
			Kind:      n.kind,
			Tenant:    n.tenant,
			Requester: "alice",
		})
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, pending.ID, got[0].ID)
	})

	t.Run("ListFiltersByExpiryBound", func(t *testing.T) {
		s, n, ctx := factory(t), newNS(), context.Background()
		now := time.Now().UTC().Truncate(time.Microsecond)

		soon := n.request("alice", approval.Pending)
		soon.ExpiresAt = now.Add(time.Minute)
		late := n.request("alice", approval.Pending)
		late.ExpiresAt = now.Add(time.Hour)
		never := n.request("alice", approval.Pending)
		for _, r := range []approval.Request{soon, late, never} {
			require.NoError(t, s.Create(ctx, r))
		}

		got, err := s.List(ctx, approval.Filter{
			Tenant:        n.tenant,
			ExpiresBefore: now.Add(10 * time.Minute),
		})
		require.NoError(t, err)
		require.Len(t, got, 1, "never-expiring rows are excluded from an expiry bound")
		assert.Equal(t, soon.ID, got[0].ID)
	})

	t.Run("ListLimitAndOrder", func(t *testing.T) {
		s, n, ctx := factory(t), newNS(), context.Background()
		var ids []id.UUID
		for range 5 {
			r := n.request("alice", approval.Pending)
			require.NoError(t, s.Create(ctx, r))
			ids = append(ids, r.ID)
			time.Sleep(2 * time.Millisecond) // UUIDv7 order is millisecond-grained
		}

		got, err := s.List(ctx, approval.Filter{Tenant: n.tenant, Limit: 2})
		require.NoError(t, err)
		require.Len(t, got, 2)
		assert.Equal(t, ids[4], got[0].ID, "newest first")
		assert.Equal(t, ids[3], got[1].ID)
	})

	t.Run("ListReturnsNonNilEmpty", func(t *testing.T) {
		s := factory(t)
		got, err := s.List(context.Background(), approval.Filter{Kind: "kind-" + id.NewUUID().String()})
		require.NoError(t, err)
		assert.NotNil(t, got, "nil vs empty must not differ across implementations")
		assert.Empty(t, got)
	})

	t.Run("ListDefaultLimitIsFixed", func(t *testing.T) {
		s, n, ctx := factory(t), newNS(), context.Background()
		for range 105 {
			require.NoError(t, s.Create(ctx, n.request("alice", approval.Pending)))
		}

		got, err := s.List(ctx, approval.Filter{Tenant: n.tenant})
		require.NoError(t, err)
		assert.Len(t, got, 100,
			"a zero Filter.Limit must default to exactly 100 across every Store implementation")
	})
}
