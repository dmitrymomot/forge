package rbac

import "testing"

func TestPermSetAllows(t *testing.T) {
	p := newPermSet([]string{"documents:read", "reports:*"})
	cases := []struct {
		action string
		want   bool
	}{
		{"documents:read", true}, // exact
		{"documents:write", false},
		{"reports:view", true}, // segment wildcard
		{"reports:export", true},
		{"reports", false}, // no colon -> not matched by "reports:*"
		{"other:read", false},
	}
	for _, c := range cases {
		if got := p.allows(c.action); got != c.want {
			t.Errorf("allows(%q) = %v, want %v", c.action, got, c.want)
		}
	}
}

func TestPermSetSuperWildcard(t *testing.T) {
	p := newPermSet([]string{"*"})
	if !p.allows("anything:here") || !p.allows("x") {
		t.Fatal("super wildcard must allow everything")
	}
}
