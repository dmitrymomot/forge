package session

import (
	"context"
	"slices"
	"time"

	"github.com/dmitrymomot/forge/auth/access"
)

// RequireElevation returns a Decider that denies the listed actions unless
// identity was re-proved within window, and abstains on everything else.
//
// The action list is mandatory: an unscoped elevation decider composed under
// access.DenyOverrides would deny every action in the application. Abstaining
// when the session IS elevated lets rbac or acl supply the Allow, so elevation
// adds a requirement rather than replacing authorization.
func RequireElevation(window time.Duration, actions ...access.Action) access.Decider {
	guarded := slices.Clone(actions)
	return access.Named("session.elevation", access.DeciderFunc(
		func(ctx context.Context, _ access.Subject, a access.Action, _ access.Resource) (access.Decision, error) {
			if !slices.Contains(guarded, a) {
				return access.Abstain.Because("action does not require elevation"), nil
			}
			inf, ok := FromContext(ctx)
			if !ok || !inf.Authenticated() {
				return access.Deny.Because("no authenticated session"), nil
			}
			if inf.ElevatedAt.IsZero() {
				return access.Deny.Because("identity has not been re-proved"), nil
			}
			if elapsed(ctx, inf.ElevatedAt) >= window {
				return access.Deny.Because("elevation expired; re-authenticate to continue"), nil
			}
			return access.Abstain.Because("elevation satisfied"), nil
		}))
}

// elapsed returns how long ago t was, using the session's clock when one is
// available so tests can drive a mock.
func elapsed(ctx context.Context, t time.Time) time.Duration {
	if s, ok := fromContext(ctx); ok && s.now != nil {
		return s.now().Sub(t)
	}
	return time.Since(t)
}
