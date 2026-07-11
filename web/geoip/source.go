package geoip

import (
	"context"
	"net/http"
	"net/netip"
)

// Locator resolves an IP address to a Location. It is the low-level primitive:
// call it directly from non-HTTP code (jobqueue handlers, CLI) that already
// holds an IP. A clean miss (IP not in the data set) is (Location{}, nil); a
// non-nil error signals a real failure (corrupt or closed database), never
// "not found".
type Locator interface {
	Lookup(ctx context.Context, ip netip.Addr) (Location, error)
}

// Source resolves a Location from an HTTP request. Header sources implement it
// directly; Middleware consumes it. Miss/error semantics match Locator: a
// header-absent or IP-miss result is (Location{}, nil); a non-nil error is a
// real failure.
type Source interface {
	Lookup(r *http.Request) (Location, error)
}
