package acl_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/access"
	"github.com/dmitrymomot/forge/auth/acl"
)

func TestMemoryEntriesForFiltering(t *testing.T) {
	s := acl.NewMemoryStore()
	ctx := context.Background()
	require.NoError(t, s.Put(ctx, "", []acl.Entry{
		{Subject: "u1", ResourceType: "agent", ResourceID: "42", Action: "agents:read", Effect: access.Allow},
		{Subject: "u1", ResourceType: "agent", ResourceID: "13", Action: "agents:read", Effect: access.Deny},
		{Subject: "u1", ResourceType: "agent", ResourceID: "", Action: "agents:list", Effect: access.Allow},
		{Subject: "u1", ResourceType: "report", ResourceID: "42", Action: "reports:read", Effect: access.Allow},
	}))

	// exact id + type-wide, same type only
	entries, err := s.EntriesFor(ctx, "", "u1", "agent", "42")
	require.NoError(t, err)
	assert.ElementsMatch(t, []acl.Entry{
		{Subject: "u1", ResourceType: "agent", ResourceID: "42", Action: "agents:read", Effect: access.Allow},
		{Subject: "u1", ResourceType: "agent", ResourceID: "", Action: "agents:list", Effect: access.Allow},
	}, entries)

	// collection check: only type-wide entries
	entries, err = s.EntriesFor(ctx, "", "u1", "agent", "")
	require.NoError(t, err)
	assert.ElementsMatch(t, []acl.Entry{
		{Subject: "u1", ResourceType: "agent", ResourceID: "", Action: "agents:list", Effect: access.Allow},
	}, entries)

	// unknown subject / tenant: empty
	entries, err = s.EntriesFor(ctx, "", "other", "agent", "42")
	require.NoError(t, err)
	assert.Empty(t, entries)
	entries, err = s.EntriesFor(ctx, "acme", "u1", "agent", "42")
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestMemoryDeleteReclaims(t *testing.T) {
	s := acl.NewMemoryStore()
	ctx := context.Background()
	require.NoError(t, s.Put(ctx, "", []acl.Entry{
		{Subject: "u1", ResourceType: "agent", ResourceID: "42", Action: "agents:read", Effect: access.Allow},
	}))
	require.NoError(t, s.Delete(ctx, "", "u1", "agent", "42", []string{"agents:read"}))
	require.NoError(t, s.Delete(ctx, "", "u1", "agent", "42", []string{"agents:read"})) // missing keys: not an error

	entries, err := s.ListFor(ctx, "", "u1")
	require.NoError(t, err)
	assert.Empty(t, entries)
}
