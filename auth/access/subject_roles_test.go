package access_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/auth/access"
	"github.com/dmitrymomot/forge/auth/guard"
)

func TestSubjectFromIdentityCopiesRoles(t *testing.T) {
	id := guard.Identity{Subject: "u1", Tenant: "acme", Scopes: []string{"s"}, Roles: []string{"admin"}}
	s := access.SubjectFromIdentity(id)
	assert.Equal(t, "u1", s.ID)
	assert.Equal(t, "acme", s.Tenant)
	assert.Equal(t, []string{"admin"}, s.Roles)
}
