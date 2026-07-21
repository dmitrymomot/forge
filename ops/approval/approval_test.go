package approval_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/ops/approval"
)

func TestStatusString(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		want string
		s    approval.Status
	}{
		{"pending", approval.Pending},
		{"approved", approval.Approved},
		{"rejected", approval.Rejected},
		{"cancelled", approval.Cancelled},
		{"expired", approval.Expired},
		{"executing", approval.Executing},
		{"executed", approval.Executed},
		{"failed", approval.Failed},
		{"unknown", approval.Status(99)},
	} {
		assert.Equal(t, tc.want, tc.s.String())
	}
}

func TestStatusTerminal(t *testing.T) {
	t.Parallel()
	terminal := []approval.Status{
		approval.Rejected, approval.Cancelled, approval.Expired,
		approval.Executed, approval.Failed,
	}
	for _, s := range terminal {
		assert.True(t, s.Terminal(), "%s must be terminal", s)
	}
	nonTerminal := []approval.Status{approval.Pending, approval.Approved, approval.Executing}
	for _, s := range nonTerminal {
		assert.False(t, s.Terminal(), "%s must not be terminal", s)
	}
}

func TestNewKind(t *testing.T) {
	t.Parallel()
	k := approval.NewKind[struct{ A int }]("payout.release")
	require.Equal(t, "payout.release", k.Name())
	assert.Panics(t, func() { approval.NewKind[int]("") }, "empty name is a wiring bug")
}
