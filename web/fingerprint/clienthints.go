package fingerprint

import (
	"net/http"
	"strings"
)

// clientHintHeaders maps a User-Agent Client-Hint request header to a component
// name. Only stable device/browser identity hints are included. Deliberately
// excluded: Sec-CH-UA-Platform-Version and -Full-Version-List churn on every
// browser update and their entropy is already in the "ua" component; Sec-Fetch-*
// are per-request context (not device identity), so they feed signals raw
// instead of being hashed as components. Note that the included "ch-ua" value
// itself carries the browser's major version (e.g. `"Chromium";v="126"`), so it
// will churn alongside "ua" on major browser updates too — a conscious
// trade-off kept for the brand-list entropy it provides.
var clientHintHeaders = []headerPair{
	{"Sec-CH-UA", "ch-ua"},
	{"Sec-CH-UA-Platform", "ch-ua-platform"},
	{"Sec-CH-UA-Mobile", "ch-ua-mobile"},
	{"Sec-CH-UA-Arch", "ch-ua-arch"},
	{"Sec-CH-UA-Bitness", "ch-ua-bitness"},
	{"Sec-CH-UA-Model", "ch-ua-model"},
	{"Device-Memory", "device-memory"},
	{"DPR", "dpr"},
}

type clientHintsCollector struct{}

// ClientHints returns a Collector contributing the request's stable User-Agent
// Client Hints (Sec-CH-UA-*, Device-Memory, DPR) as components. Absent or blank
// hints contribute nothing. Modern Chromium browsers send these; others omit
// them, so this layer adds entropy only where available.
func ClientHints() Collector { return clientHintsCollector{} }

func (clientHintsCollector) Collect(r *http.Request) ([]Component, error) {
	comps := make([]Component, 0, len(clientHintHeaders))
	for _, h := range clientHintHeaders {
		if v := strings.TrimSpace(r.Header.Get(h.header)); v != "" {
			comps = append(comps, Component{Name: h.name, Value: v})
		}
	}
	return comps, nil
}
