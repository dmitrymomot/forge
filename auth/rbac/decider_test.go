package rbac_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/access"
	"github.com/dmitrymomot/forge/auth/rbac"
)

func TestDeciderFromSubjectAllowAbstain(t *testing.T) {
	rs := mkSet(t)
	d := rbac.Decider(rs, rbac.FromSubject())
	ctx := context.Background()

	dec, err := d.Decide(ctx, access.Subject{ID: "u1", Roles: []string{"editor"}}, "documents:write", access.Resource{})
	require.NoError(t, err)
	assert.Equal(t, access.Allow, dec.Effect)

	dec, err = d.Decide(ctx, access.Subject{ID: "u1", Roles: []string{"viewer"}}, "documents:write", access.Resource{})
	require.NoError(t, err)
	assert.Equal(t, access.Abstain, dec.Effect) // missing grant -> abstain, never deny
}

func TestDeciderFromStore(t *testing.T) {
	rs := mkSet(t)
	store := rbac.NewMemoryStore()
	require.NoError(t, store.Assign(context.Background(), "", "u1", []string{"admin"}))

	d := rbac.Decider(rs, rbac.FromStore(store))
	dec, err := d.Decide(context.Background(), access.Subject{ID: "u1"}, "anything:here", access.Resource{})
	require.NoError(t, err)
	assert.Equal(t, access.Allow, dec.Effect) // admin has "*"
}

func TestDeciderAbstainClosesToDenyViaAuthorize(t *testing.T) {
	rs := mkSet(t)
	d := rbac.Decider(rs, rbac.FromSubject())
	dec, err := access.Authorize(context.Background(), d, access.Subject{ID: "u1", Roles: []string{"viewer"}}, "documents:write", access.Resource{})
	require.NoError(t, err)
	assert.Equal(t, access.Deny, dec.Effect) // Authorize turns all-abstain into fail-closed deny
}
