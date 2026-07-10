package ipfilter

import (
	"errors"

	"github.com/dmitrymomot/forge/core/errorsx"
)

// ErrInvalidCIDR is wrapped in the panic value New raises when a WithAllow or
// WithDeny entry is not a parseable CIDR or IP address.
var ErrInvalidCIDR = errors.New("ipfilter: invalid CIDR")

// ErrBlocked is passed to the responder when a request's client IP is filtered.
var ErrBlocked = errorsx.New("ip_blocked", "client address not allowed")
