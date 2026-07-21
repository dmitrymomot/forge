package approval_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/access"
	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/ops/approval"
	"github.com/dmitrymomot/forge/ops/auditlog"
)

type tenantKey struct{}

func ctxTenant(t string) context.Context {
	return context.WithValue(context.Background(), tenantKey{}, t)
}

func tenantFromCtx(ctx context.Context) (string, error) {
	t, _ := ctx.Value(tenantKey{}).(string)
	return t, nil
}

func scopedManager(t *testing.T, store approval.Store) *approval.Manager {
	t.Helper()
	return approval.New(store,
		approval.WithKind(kindPayout, approval.Policy{Quorum: 2}),
		approval.WithClock(clock.NewMock(fixedNow)),
		approval.WithScope(tenantFromCtx))
}

func TestScopeStampsTenantOnSubmit(t *testing.T) {
	t.Parallel()
	m := scopedManager(t, approval.NewMemoryStore())

	r, err := approval.Submit(ctxTenant("acme"), m, kindPayout, payoutPayload{},
		approval.SubmitParams{Requester: "alice"})
	require.NoError(t, err)
	assert.Equal(t, "acme", r.Tenant)
}

func TestScopeFailsClosedOnEmptyTenant(t *testing.T) {
	t.Parallel()
	m := scopedManager(t, approval.NewMemoryStore())

	_, err := approval.Submit(context.Background(), m, kindPayout, payoutPayload{},
		approval.SubmitParams{Requester: "alice"})
	assert.ErrorIs(t, err, approval.ErrScope, "a missing tenant must not fall back to global")
}

func TestScopeFailsClosedOnHookError(t *testing.T) {
	t.Parallel()
	m := approval.New(approval.NewMemoryStore(),
		approval.WithKind(kindPayout, approval.Policy{Quorum: 2}),
		approval.WithScope(func(context.Context) (string, error) {
			return "", errors.New("tenant lookup failed")
		}))

	_, err := approval.Submit(context.Background(), m, kindPayout, payoutPayload{},
		approval.SubmitParams{Requester: "alice"})
	assert.ErrorIs(t, err, approval.ErrScope)
}

func TestScopeRejectsDisagreeingExplicitTenant(t *testing.T) {
	t.Parallel()
	m := scopedManager(t, approval.NewMemoryStore())

	_, err := approval.Submit(ctxTenant("acme"), m, kindPayout, payoutPayload{},
		approval.SubmitParams{Requester: "alice", Tenant: "globex"})
	assert.ErrorIs(t, err, approval.ErrScope)

	// Agreeing is fine.
	_, err = approval.Submit(ctxTenant("acme"), m, kindPayout, payoutPayload{},
		approval.SubmitParams{Requester: "alice", Tenant: "acme"})
	assert.NoError(t, err)
}

func TestCrossTenantAccessReportsNotFound(t *testing.T) {
	t.Parallel()
	store := approval.NewMemoryStore()
	m := scopedManager(t, store)

	r, err := approval.Submit(ctxTenant("acme"), m, kindPayout, payoutPayload{},
		approval.SubmitParams{Requester: "alice"})
	require.NoError(t, err)

	other := ctxTenant("globex")
	_, err = m.Approve(other, r.ID, actor("bob"))
	assert.ErrorIs(t, err, approval.ErrNotFound,
		"not ErrScope — cross-tenant existence must not be probeable")

	_, err = m.Cancel(other, r.ID, actor("bob"))
	assert.ErrorIs(t, err, approval.ErrNotFound)
}

func TestListIsConfinedToScope(t *testing.T) {
	t.Parallel()
	store := approval.NewMemoryStore()
	m := scopedManager(t, store)

	_, err := approval.Submit(ctxTenant("acme"), m, kindPayout, payoutPayload{},
		approval.SubmitParams{Requester: "alice"})
	require.NoError(t, err)
	_, err = approval.Submit(ctxTenant("globex"), m, kindPayout, payoutPayload{},
		approval.SubmitParams{Requester: "carol"})
	require.NoError(t, err)

	got, err := m.List(ctxTenant("acme"), approval.Filter{})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "acme", got[0].Tenant)
}

func TestUnscopedManagerPaysNoCeremony(t *testing.T) {
	t.Parallel()
	m, r := submitted(t, approval.Policy{Quorum: 1})
	assert.Empty(t, r.Tenant)

	got, err := m.Approve(context.Background(), r.ID, actor("bob"))
	require.NoError(t, err)
	assert.Equal(t, approval.Approved, got.Status)
}

func TestScopedGetReportsNotFoundForOtherTenant(t *testing.T) {
	t.Parallel()
	store := approval.NewMemoryStore()
	m := scopedManager(t, store)

	r, err := approval.Submit(ctxTenant("acme"), m, kindPayout, payoutPayload{},
		approval.SubmitParams{Requester: "alice"})
	require.NoError(t, err)

	_, err = m.Get(ctxTenant("acme"), r.ID)
	require.NoError(t, err, "same-tenant Get must still work")

	_, err = m.Get(ctxTenant("globex"), r.ID)
	assert.ErrorIs(t, err, approval.ErrNotFound,
		"cross-tenant Get must not leak the record or a distinguishable forbidden error")
}

func TestDeniedAttemptAuditCarriesTenant(t *testing.T) {
	t.Parallel()
	sink := auditlog.NewMemorySink()
	d := &recordingDecider{effect: access.Deny}
	m := approval.New(approval.NewMemoryStore(),
		approval.WithKind(kindPayout, approval.Policy{Quorum: 2}),
		approval.WithClock(clock.NewMock(fixedNow)),
		approval.WithScope(tenantFromCtx),
		approval.WithDecider(d),
		approval.WithAuditor(auditlog.New(sink)))

	r, err := approval.Submit(ctxTenant("acme"), m, kindPayout, payoutPayload{},
		approval.SubmitParams{Requester: "alice"})
	require.NoError(t, err)

	_, err = m.Approve(ctxTenant("acme"), r.ID, actor("mallory"))
	require.ErrorIs(t, err, approval.ErrNotEligible)

	events := sink.Events()
	require.Len(t, events, 2, "submit + the denied attempt")
	assert.Equal(t, auditlog.OutcomeDenied, events[1].Outcome)
	assert.Equal(t, "acme", events[1].Tenant,
		"a denied attempt must be attributable to its tenant in the trail")
}
