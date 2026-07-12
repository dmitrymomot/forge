package fingerprint

import (
	"net/http"
	"strings"
)

// headerPair maps a request header to a component name.
type headerPair struct{ header, name string }

var fingerprintHeaders = []headerPair{
	{"User-Agent", "ua"},
	{"Accept", "accept"},
	{"Accept-Language", "accept-language"},
	{"Accept-Encoding", "accept-encoding"},
}

type headersCollector struct{}

// Headers returns a Collector contributing the request's User-Agent and Accept*
// headers as components. Absent or blank headers contribute nothing.
func Headers() Collector { return headersCollector{} }

func (headersCollector) Collect(r *http.Request) ([]Component, error) {
	comps := make([]Component, 0, len(fingerprintHeaders))
	for _, h := range fingerprintHeaders {
		if v := strings.TrimSpace(r.Header.Get(h.header)); v != "" {
			comps = append(comps, Component{Name: h.name, Value: v})
		}
	}
	return comps, nil
}
