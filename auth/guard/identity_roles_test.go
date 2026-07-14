package guard_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/auth/guard"
)

func TestIdentityCarriesRoles(t *testing.T) {
	id := guard.Identity{Subject: "u1", Roles: []string{"admin", "editor"}}
	assert.Equal(t, []string{"admin", "editor"}, id.Roles)
}
