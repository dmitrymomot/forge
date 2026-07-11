package geoip

import (
	"context"
	"net/http"
	"net/netip"

	"github.com/dmitrymomot/forge/web/clientip"
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

type chainSource struct{ sources []Source }

// Chain returns a Source that queries each source in order and returns the
// first non-empty Location. A source returning an error is skipped (one broken
// source never blanks the chain); if every source misses or errors, the first
// error encountered is returned so the caller (e.g. Middleware) can log it.
func Chain(sources ...Source) Source { return chainSource{sources: sources} }

func (c chainSource) Lookup(r *http.Request) (Location, error) {
	var firstErr error
	for _, s := range c.sources {
		loc, err := s.Lookup(r)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if !loc.Empty() {
			return loc, nil
		}
	}
	return Location{}, firstErr
}

type locatorSource struct {
	loc  Locator
	opts []clientip.Option
}

// FromLocator adapts an IP-based Locator into a request Source by resolving the
// client IP first. With no options it uses clientip.Get (honoring an installed
// clientip.Middleware); with options it uses clientip.Resolve(r, opts...). The
// resolved IP is looked up via loc.Lookup(r.Context(), ip). A request whose IP
// does not parse returns (Location{}, nil).
func FromLocator(loc Locator, opts ...clientip.Option) Source {
	return locatorSource{loc: loc, opts: opts}
}

func (s locatorSource) Lookup(r *http.Request) (Location, error) {
	ipStr := clientip.Get(r)
	if len(s.opts) > 0 {
		ipStr = clientip.Resolve(r, s.opts...)
	}
	ip, err := netip.ParseAddr(ipStr)
	if err != nil {
		return Location{}, nil
	}
	return s.loc.Lookup(r.Context(), ip)
}
