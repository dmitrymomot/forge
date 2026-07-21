package approval_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/ops/approval"
)

func TestGetAppliesLazyExpiry(t *testing.T) {
	t.Parallel()
	clk := clock.NewMock(fixedNow)
	m := approval.New(approval.NewMemoryStore(),
		approval.WithKind(kindPayout, approval.Policy{Quorum: 2, TTL: time.Hour}),
		approval.WithClock(clk))
	ctx := context.Background()

	r, err := approval.Submit(ctx, m, kindPayout, payoutPayload{},
		approval.SubmitParams{Requester: "alice"})
	require.NoError(t, err)

	got, err := m.Get(ctx, r.ID)
	require.NoError(t, err)
	assert.Equal(t, approval.Pending, got.Status)

	clk.Set(fixedNow.Add(time.Hour)) // exactly at ExpiresAt
	got, err = m.Get(ctx, r.ID)
	require.NoError(t, err)
	assert.Equal(t, approval.Expired, got.Status, "expiry is inclusive of ExpiresAt")
}

func TestZeroTTLNeverExpires(t *testing.T) {
	t.Parallel()
	clk := clock.NewMock(fixedNow)
	m := approval.New(approval.NewMemoryStore(),
		approval.WithKind(kindPayout, approval.Policy{Quorum: 2}),
		approval.WithClock(clk))
	ctx := context.Background()

	r, err := approval.Submit(ctx, m, kindPayout, payoutPayload{},
		approval.SubmitParams{Requester: "alice"})
	require.NoError(t, err)

	clk.Set(fixedNow.Add(10 * 365 * 24 * time.Hour))
	got, err := m.Get(ctx, r.ID)
	require.NoError(t, err)
	assert.Equal(t, approval.Pending, got.Status)
}

func TestListDropsRecordsExpiredOutOfTheFilter(t *testing.T) {
	t.Parallel()
	clk := clock.NewMock(fixedNow)
	m := approval.New(approval.NewMemoryStore(),
		approval.WithKind(kindPayout, approval.Policy{Quorum: 2, TTL: time.Hour}),
		approval.WithClock(clk))
	ctx := context.Background()

	_, err := approval.Submit(ctx, m, kindPayout, payoutPayload{},
		approval.SubmitParams{Requester: "alice"})
	require.NoError(t, err)

	got, err := m.List(ctx, approval.Filter{Statuses: []approval.Status{approval.Pending}})
	require.NoError(t, err)
	require.Len(t, got, 1)

	clk.Set(fixedNow.Add(2 * time.Hour))
	got, err = m.List(ctx, approval.Filter{Statuses: []approval.Status{approval.Pending}})
	require.NoError(t, err)
	assert.Empty(t, got, "the stored row is still Pending but reads as Expired")

	// Unfiltered, it still comes back — carrying the derived status.
	got, err = m.List(ctx, approval.Filter{})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, approval.Expired, got[0].Status)
}
