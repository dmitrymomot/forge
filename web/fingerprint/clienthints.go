package fingerprint

import (
	"net/http"
	"strings"

	"github.com/dmitrymomot/forge/web/middleware"
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
//
// Entropy tiers matter for what actually arrives: only the first three
// (Sec-CH-UA, Sec-CH-UA-Platform, Sec-CH-UA-Mobile) are low-entropy hints that
// Chromium sends on every request unprompted. Sec-CH-UA-Arch, -Bitness, -Model,
// Device-Memory, and DPR are high-entropy: the browser withholds them until the
// origin advertises them via Accept-CH (see AcceptCH), and even then only sends
// them on subsequent requests to that origin. Without AcceptCH, those five
// components essentially never populate from headers — the JS probe's js-uadata
// (navigator.userAgentData.getHighEntropyValues) is the client-side alternative
// for arch/model/bitness.
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

// acceptCHValue is the Accept-CH header value advertising every hint the
// ClientHints collector reads, derived from clientHintHeaders so the collector
// and the advertisement can never drift out of sync.
var acceptCHValue = func() string {
	names := make([]string, len(clientHintHeaders))
	for i := range clientHintHeaders {
		names[i] = clientHintHeaders[i].header
	}
	return strings.Join(names, ", ")
}()

type clientHintsCollector struct{}

// ClientHints returns a Collector contributing the request's User-Agent Client
// Hints (Sec-CH-UA-*, Device-Memory, DPR) as components. Absent or blank hints
// contribute nothing. Only the low-entropy hints (ch-ua, ch-ua-platform,
// ch-ua-mobile) arrive on every Chromium request; the high-entropy ones
// (ch-ua-arch, ch-ua-bitness, ch-ua-model, device-memory, dpr) require the
// origin to opt in with Accept-CH — pair this with AcceptCH to collect them, or
// rely on the JS probe's js-uadata. Non-Chromium browsers send none of these.
func ClientHints() Collector { return clientHintsCollector{} }

// AcceptCH returns middleware that advertises, via the Accept-CH response
// header, every Client Hint the ClientHints collector reads. Without it Chromium
// sends only the three low-entropy hints (Sec-CH-UA, Sec-CH-UA-Mobile,
// Sec-CH-UA-Platform); the high-entropy hints (Sec-CH-UA-Arch, -Bitness, -Model,
// Device-Memory, DPR) are withheld until the origin opts in here, and then only
// arrive on subsequent requests to that origin. Add a Critical-CH header if you
// need them on the first navigation, and a Vary header if a hint affects a
// cached response.
func AcceptCH() middleware.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Accept-CH", acceptCHValue)
			next.ServeHTTP(w, r)
		})
	}
}

func (clientHintsCollector) Collect(r *http.Request) ([]Component, error) {
	comps := make([]Component, 0, len(clientHintHeaders))
	for _, h := range clientHintHeaders {
		if v := strings.TrimSpace(r.Header.Get(h.header)); v != "" {
			comps = append(comps, Component{Name: h.name, Value: v})
		}
	}
	return comps, nil
}
