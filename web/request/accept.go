package request

import (
	"net/http"
	"strconv"
	"strings"
)

// Accept reports whether the request's Accept header admits mediaType, honoring
// "*/*", "type/*", and explicit "q=0" rejections. Per RFC 9110 §12.5.1 the most
// specific matching range decides: an exact "type/subtype" outranks "type/*",
// which outranks "*/*", so an explicit "q=0" exclusion overrides a less-specific
// wildcard (e.g. "*/*, application/json;q=0" rejects application/json). Among
// ranges of equal specificity the highest q-value wins. An absent or empty Accept
// header admits everything (returns true).
func Accept(r *http.Request, mediaType string) bool {
	header := r.Header.Get("Accept")
	if strings.TrimSpace(header) == "" {
		return true
	}
	want := strings.SplitN(strings.ToLower(strings.TrimSpace(mediaType)), "/", 2)
	if len(want) != 2 {
		return false
	}
	bestSpec := -1
	bestQ := 0.0
	for part := range strings.SplitSeq(header, ",") {
		rng, q := parseAcceptPart(part)
		if rng == "" {
			continue
		}
		spec := acceptSpecificity(rng, want)
		if spec < 0 {
			continue // range does not match mediaType
		}
		// A more specific range takes precedence regardless of q; among equally
		// specific ranges the higher q wins.
		if spec > bestSpec || (spec == bestSpec && q > bestQ) {
			bestSpec, bestQ = spec, q
		}
	}
	return bestSpec >= 0 && bestQ > 0
}

// AcceptsJSON reports whether the request admits application/json.
func AcceptsJSON(r *http.Request) bool { return Accept(r, "application/json") }

// parseAcceptPart splits one Accept list element into its lowercased media range
// and q-value (default 1.0).
func parseAcceptPart(part string) (string, float64) {
	fields := strings.Split(part, ";")
	rng := strings.ToLower(strings.TrimSpace(fields[0]))
	q := 1.0
	for _, p := range fields[1:] {
		if v, ok := strings.CutPrefix(strings.TrimSpace(p), "q="); ok {
			if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
				q = f
			}
		}
	}
	return rng, q
}

// acceptSpecificity reports how specifically a media range matches the wanted
// type/subtype, or -1 if it does not match: 2 for an exact "type/subtype", 1 for
// "type/*", 0 for "*/*". Higher wins per RFC 9110 §12.5.1.
func acceptSpecificity(rng string, want []string) int {
	if rng == "*/*" {
		return 0
	}
	rp := strings.SplitN(rng, "/", 2)
	if len(rp) != 2 || rp[0] != want[0] {
		return -1
	}
	switch rp[1] {
	case want[1]:
		return 2
	case "*":
		return 1
	default:
		return -1
	}
}
