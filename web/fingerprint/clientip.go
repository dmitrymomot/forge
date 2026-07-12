package fingerprint

import (
	"net/http"

	"github.com/dmitrymomot/forge/web/clientip"
)

type ipCollector struct{ opts []clientip.Option }

// ClientIP returns a Collector contributing the resolved client IP as the "ip"
// component. With no options it uses clientip.Get (honoring an installed
// clientip.Middleware); with options it resolves via clientip.Resolve. A request
// whose IP cannot be resolved contributes nothing.
func ClientIP(opts ...clientip.Option) Collector { return ipCollector{opts: opts} }

func (c ipCollector) Collect(r *http.Request) ([]Component, error) {
	ip := clientip.Get(r)
	if len(c.opts) > 0 {
		ip = clientip.Resolve(r, c.opts...)
	}
	if ip == "" {
		return nil, nil
	}
	return []Component{{Name: "ip", Value: ip}}, nil
}
