package approval_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/ops/approval"
)

type payoutPayload struct {
	PayoutID string `json:"payout_id"`
	Amount   int64  `json:"amount"`
}

var kindPayout = approval.NewKind[payoutPayload]("payout.release")

func TestNewPanics(t *testing.T) {
	t.Parallel()

	assert.PanicsWithValue(t, "approval: nil store", func() {
		approval.New(nil, approval.WithKind(kindPayout, approval.Policy{Quorum: 2}))
	})

	assert.Panics(t, func() {
		approval.New(approval.NewMemoryStore(),
			approval.WithKind(kindPayout, approval.Policy{Quorum: 0}))
	}, "quorum below 1 is a wiring bug")

	assert.Panics(t, func() {
		approval.New(approval.NewMemoryStore(),
			approval.WithKind(kindPayout, approval.Policy{Quorum: 2}),
			approval.WithKind(kindPayout, approval.Policy{Quorum: 3}))
	}, "duplicate kind registration is a wiring bug")

	assert.Panics(t, func() {
		approval.New(approval.NewMemoryStore(),
			approval.WithKind(kindPayout, approval.Policy{Quorum: 2, TTL: -time.Hour}))
	}, "negative TTL is a wiring bug")

	assert.Panics(t, func() {
		approval.New(approval.NewMemoryStore(),
			approval.WithKind(kindPayout, approval.Policy{Quorum: 2, ClaimTTL: -time.Hour}))
	}, "negative ClaimTTL is a wiring bug")

	assert.Panics(t, func() {
		approval.New(approval.NewMemoryStore())
	}, "a manager with no registered kinds can never accept a submission")
}

func TestNewAcceptsValidWiring(t *testing.T) {
	t.Parallel()
	m := approval.New(approval.NewMemoryStore(),
		approval.WithKind(kindPayout, approval.Policy{Quorum: 2, TTL: 24 * time.Hour}))
	require.NotNil(t, m)
}
