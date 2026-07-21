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

func TestManagerGrantListRevoke(t *testing.T) {
	m := acl.NewManager(acl.NewMemoryStore())
	ctx := context.Background()

	require.NoError(t, m.Grant(ctx, "u1", "agent", "42", "agents:read", "agents:write"))
	require.NoError(t, m.Grant(ctx, "u1", "agent", "42", "agents:read")) // idempotent
	require.NoError(t, m.Deny(ctx, "u1", "report", "", "reports:export"))

	entries, err := m.ListFor(ctx, "u1")
	require.NoError(t, err)
	assert.ElementsMatch(t, []acl.Entry{
		{Subject: "u1", ResourceType: "agent", ResourceID: "42", Action: "agents:read", Effect: access.Allow},
		{Subject: "u1", ResourceType: "agent", ResourceID: "42", Action: "agents:write", Effect: access.Allow},
		{Subject: "u1", ResourceType: "report", ResourceID: "", Action: "reports:export", Effect: access.Deny},
	}, entries)

	// Revoke removes grants and denies alike
	require.NoError(t, m.Revoke(ctx, "u1", "agent", "42", "agents:write"))
	require.NoError(t, m.Revoke(ctx, "u1", "report", "", "reports:export"))
	entries, err = m.ListFor(ctx, "u1")
	require.NoError(t, err)
	assert.ElementsMatch(t, []acl.Entry{
		{Subject: "u1", ResourceType: "agent", ResourceID: "42", Action: "agents:read", Effect: access.Allow},
	}, entries)
}

func TestManagerGrantDenyFlipsInPlace(t *testing.T) {
	m := acl.NewManager(acl.NewMemoryStore())
	ctx := context.Background()

	require.NoError(t, m.Grant(ctx, "u1", "agent", "42", "agents:read"))
	require.NoError(t, m.Deny(ctx, "u1", "agent", "42", "agents:read"))

	entries, err := m.ListFor(ctx, "u1")
	require.NoError(t, err)
	require.Len(t, entries, 1) // one key — no contradictory grant+deny pair
	assert.Equal(t, access.Deny, entries[0].Effect)

	require.NoError(t, m.Grant(ctx, "u1", "agent", "42", "agents:read"))
	entries, err = m.ListFor(ctx, "u1")
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, access.Allow, entries[0].Effect)
}

func TestManagerValidation(t *testing.T) {
	m := acl.NewManager(acl.NewMemoryStore())
	ctx := context.Background()

	assert.ErrorIs(t, m.Grant(ctx, "", "agent", "42", "agents:read"), acl.ErrInvalidEntry)
	assert.ErrorIs(t, m.Grant(ctx, "u1", "", "42", "agents:read"), acl.ErrInvalidEntry)
	assert.ErrorIs(t, m.Deny(ctx, "u1", "agent", "42", ""), acl.ErrInvalidEntry)

	// zero actions: no-op, nothing written
	require.NoError(t, m.Grant(ctx, "u1", "agent", "42"))
	require.NoError(t, m.Revoke(ctx, "u1", "agent", "42"))
	entries, err := m.ListFor(ctx, "u1")
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestManagerScopeIsolation(t *testing.T) {
	store := acl.NewMemoryStore()
	tenant := "acme"
	m := acl.NewManager(store, acl.WithScope(func(context.Context) (string, error) {
		return tenant, nil
	}))
	ctx := context.Background()
	require.NoError(t, m.Grant(ctx, "u1", "agent", "42", "agents:read"))

	// another tenant sees nothing
	tenant = "other"
	entries, err := m.ListFor(ctx, "u1")
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestManagerScopeFailClosed(t *testing.T) {
	boom := errors.New("boom")
	for name, hook := range map[string]func(context.Context) (string, error){
		"hook error":   func(context.Context) (string, error) { return "", boom },
		"empty tenant": func(context.Context) (string, error) { return "", nil },
	} {
		t.Run(name, func(t *testing.T) {
			m := acl.NewManager(acl.NewMemoryStore(), acl.WithScope(hook))
			ctx := context.Background()

			assert.ErrorIs(t, m.Grant(ctx, "u1", "agent", "42", "agents:read"), acl.ErrScope)
			assert.ErrorIs(t, m.Deny(ctx, "u1", "agent", "42", "agents:read"), acl.ErrScope)
			assert.ErrorIs(t, m.Revoke(ctx, "u1", "agent", "42", "agents:read"), acl.ErrScope)
			_, err := m.ListFor(ctx, "u1")
			assert.ErrorIs(t, err, acl.ErrScope)
		})
	}
}
