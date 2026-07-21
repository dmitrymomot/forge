package acl_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/access"
	"github.com/dmitrymomot/forge/auth/acl"
)

// fakeStore returns canned entries and records the EntriesFor arguments.
type fakeStore struct {
	err     error
	entries []acl.Entry
	got     [4]string // tenant, subject, resourceType, resourceID
}

func (f *fakeStore) EntriesFor(_ context.Context, tenant, subject, resourceType, resourceID string) ([]acl.Entry, error) {
	f.got = [4]string{tenant, subject, resourceType, resourceID}
	return f.entries, f.err
}

func (f *fakeStore) ListFor(context.Context, string, string) ([]acl.Entry, error) { return nil, nil }
func (f *fakeStore) Put(context.Context, string, []acl.Entry) error               { return nil }
func (f *fakeStore) Delete(context.Context, string, string, string, string, []string) error {
	return nil
}

func decide(t *testing.T, store acl.Store, sub access.Subject, action access.Action, res access.Resource) access.Decision {
	t.Helper()
	dec, err := acl.Decider(store).Decide(context.Background(), sub, action, res)
	require.NoError(t, err)
	return dec
}

func TestDeciderGrantDenyAbstain(t *testing.T) {
	ctx := context.Background()
	store := acl.NewMemoryStore()
	m := acl.NewManager(store)
	require.NoError(t, m.Grant(ctx, "mgr-7", "agent", "42", "agents:read"))
	require.NoError(t, m.Deny(ctx, "mgr-7", "agent", "13", "agents:read"))

	sub := access.Subject{ID: "mgr-7"}

	dec := decide(t, store, sub, "agents:read", access.Resource{Type: "agent", ID: "42"})
	assert.Equal(t, access.Allow, dec.Effect)
	assert.Equal(t, "acl", dec.Decider)

	dec = decide(t, store, sub, "agents:read", access.Resource{Type: "agent", ID: "13"})
	assert.Equal(t, access.Deny, dec.Effect)

	// unlisted resource, unlisted action, unlisted subject: no opinion
	assert.Equal(t, access.Abstain, decide(t, store, sub, "agents:read", access.Resource{Type: "agent", ID: "99"}).Effect)
	assert.Equal(t, access.Abstain, decide(t, store, sub, "agents:write", access.Resource{Type: "agent", ID: "42"}).Effect)
	assert.Equal(t, access.Abstain, decide(t, store, access.Subject{ID: "other"}, "agents:read", access.Resource{Type: "agent", ID: "42"}).Effect)
}

func TestDeciderDenyWinsOverGrant(t *testing.T) {
	ctx := context.Background()
	store := acl.NewMemoryStore()
	m := acl.NewManager(store)
	// type-wide suspension beats a specific grant regardless of entry order
	require.NoError(t, m.Grant(ctx, "u1", "document", "7", "documents:read"))
	require.NoError(t, m.Deny(ctx, "u1", "document", "", "documents:read"))

	dec := decide(t, store, access.Subject{ID: "u1"}, "documents:read", access.Resource{Type: "document", ID: "7"})
	assert.Equal(t, access.Deny, dec.Effect)
}

func TestDeciderActionWildcard(t *testing.T) {
	ctx := context.Background()
	store := acl.NewMemoryStore()
	m := acl.NewManager(store)
	require.NoError(t, m.Grant(ctx, "u1", "agent", "42", "*"))

	sub := access.Subject{ID: "u1"}
	assert.Equal(t, access.Allow, decide(t, store, sub, "agents:read", access.Resource{Type: "agent", ID: "42"}).Effect)
	assert.Equal(t, access.Allow, decide(t, store, sub, "agents:delete", access.Resource{Type: "agent", ID: "42"}).Effect)
	assert.Equal(t, access.Abstain, decide(t, store, sub, "agents:read", access.Resource{Type: "agent", ID: "43"}).Effect)
}

func TestDeciderCollectionCheck(t *testing.T) {
	ctx := context.Background()
	store := acl.NewMemoryStore()
	m := acl.NewManager(store)
	require.NoError(t, m.Grant(ctx, "u1", "agent", "42", "agents:read")) // specific
	require.NoError(t, m.Grant(ctx, "u1", "report", "", "reports:read")) // type-wide

	sub := access.Subject{ID: "u1"}
	// a collection check (ID "") is matched only by type-wide entries
	assert.Equal(t, access.Abstain, decide(t, store, sub, "agents:read", access.Resource{Type: "agent"}).Effect)
	assert.Equal(t, access.Allow, decide(t, store, sub, "reports:read", access.Resource{Type: "report"}).Effect)
}

func TestDeciderRefiltersOverReturningStore(t *testing.T) {
	// A loose Store may over-return; grants must not leak onto other resources,
	// types, or actions.
	store := &fakeStore{entries: []acl.Entry{
		{Subject: "u1", ResourceType: "agent", ResourceID: "42", Action: "agents:read", Effect: access.Allow},
		{Subject: "u1", ResourceType: "report", ResourceID: "1", Action: "reports:read", Effect: access.Allow},
	}}
	sub := access.Subject{ID: "u1"}

	assert.Equal(t, access.Abstain, decide(t, store, sub, "agents:read", access.Resource{Type: "agent", ID: "13"}).Effect)
	assert.Equal(t, access.Abstain, decide(t, store, sub, "reports:read", access.Resource{Type: "agent", ID: "42"}).Effect)
	assert.Equal(t, access.Allow, decide(t, store, sub, "agents:read", access.Resource{Type: "agent", ID: "42"}).Effect)
}

func TestDeciderSkipsNonDecisiveEffects(t *testing.T) {
	store := &fakeStore{entries: []acl.Entry{
		{Subject: "u1", ResourceType: "agent", ResourceID: "42", Action: "agents:read"}, // zero Effect (Abstain)
	}}
	dec := decide(t, store, access.Subject{ID: "u1"}, "agents:read", access.Resource{Type: "agent", ID: "42"})
	assert.Equal(t, access.Abstain, dec.Effect)
}

func TestDeciderStoreErrorFailsClosed(t *testing.T) {
	boom := errors.New("boom")
	store := &fakeStore{err: boom}
	d := acl.Decider(store)

	dec, err := d.Decide(context.Background(), access.Subject{ID: "u1"}, "agents:read", access.Resource{Type: "agent", ID: "42"})
	require.ErrorIs(t, err, boom)
	assert.Equal(t, access.Abstain, dec.Effect)

	// Authorize closes it: error → Deny
	dec, err = access.Authorize(context.Background(), d, access.Subject{ID: "u1"}, "agents:read", access.Resource{Type: "agent", ID: "42"})
	require.ErrorIs(t, err, boom)
	assert.Equal(t, access.Deny, dec.Effect)
}

func TestDeciderPassesSubjectTenant(t *testing.T) {
	store := &fakeStore{}
	sub := access.Subject{ID: "u1", Tenant: "acme"}
	_ = decide(t, store, sub, "agents:read", access.Resource{Type: "agent", ID: "42", Tenant: "acme"})
	assert.Equal(t, [4]string{"acme", "u1", "agent", "42"}, store.got)
}

func TestDeciderTenantIsolation(t *testing.T) {
	ctx := context.Background()
	store := acl.NewMemoryStore()
	tenant := "acme"
	m := acl.NewManager(store, acl.WithScope(func(context.Context) (string, error) { return tenant, nil }))
	require.NoError(t, m.Grant(ctx, "u1", "agent", "42", "agents:read"))

	res := access.Resource{Type: "agent", ID: "42"}
	assert.Equal(t, access.Allow, decide(t, store, access.Subject{ID: "u1", Tenant: "acme"}, "agents:read", res).Effect)
	assert.Equal(t, access.Abstain, decide(t, store, access.Subject{ID: "u1", Tenant: "other"}, "agents:read", res).Effect)
	assert.Equal(t, access.Abstain, decide(t, store, access.Subject{ID: "u1"}, "agents:read", res).Effect)
}
