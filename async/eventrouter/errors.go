package eventrouter

import (
	"errors"
	"fmt"
)

var (
	// ErrPermanent marks a delivery failure retrying cannot fix: the
	// destination rejected the payload, not the moment. The router
	// dead-letters instead of retrying; deliverers wrap with Permanent.
	ErrPermanent = errors.New("eventrouter: permanent delivery failure")

	// ErrScopeMissing is returned as the delivery verdict when a destination's
	// scope hook is configured and yields an error or empty scope — fail
	// closed, the event retries instead of joining a cross-tenant batch.
	ErrScopeMissing = errors.New("eventrouter: scope missing")

	// ErrInvalidURL is returned by NewHTTPDeliverer when the destination URL
	// is not absolute http(s).
	ErrInvalidURL = errors.New("eventrouter: invalid destination URL")
)

// Permanent wraps err into a Deliverer verdict: the destination rejected this
// delivery and retrying cannot help, so the router dead-letters instead of
// retrying (isolating the poison event first on multi-event batches).
// Permanent(nil) returns nil.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %w", ErrPermanent, err)
}
