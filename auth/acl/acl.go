package acl

import "github.com/dmitrymomot/forge/auth/access"

// Entry is one ACL override: Subject may (Effect Allow) or may not (Effect
// Deny) perform Action on the addressed resource. ResourceID "" makes the
// entry type-wide — it matches every resource of ResourceType, collection
// checks included. Action is the exact action string, or "*" for every action
// on the resource. Any other Effect (including the zero value Abstain) never
// matches, so a zero Entry grants nothing.
type Entry struct {
	Subject      string
	ResourceType string
	ResourceID   string
	Action       string
	Effect       access.Effect
}

// matches reports whether the entry applies to (resourceType, resourceID,
// action). The Decider re-filters with it so an over-returning Store can
// never widen a grant onto the wrong resource.
func (e Entry) matches(resourceType, resourceID string, action access.Action) bool {
	if e.ResourceType != resourceType {
		return false
	}
	if e.ResourceID != "" && e.ResourceID != resourceID {
		return false
	}
	return e.Action == "*" || e.Action == string(action)
}
