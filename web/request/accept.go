package request

import (
	"net/http"
	"strconv"
	"strings"
)

// Accept reports whether the request's Accept header admits mediaType, honoring
// "*/*", "type/*", and explicit "q=0" rejections. An absent or empty Accept header
// admits everything (returns true), per RFC 9110.
func Accept(r *http.Request, mediaType string) bool {
	header := r.Header.Get("Accept")
	if strings.TrimSpace(header) == "" {
		return true
	}
	want := strings.SplitN(strings.ToLower(strings.TrimSpace(mediaType)), "/", 2)
	if len(want) != 2 {
		return false
	}
	best := -1.0
	for part := range strings.SplitSeq(header, ",") {
		rng, q := parseAcceptPart(part)
		if rng == "" {
			continue
		}
		if acceptMatches(rng, want) && q > best {
			best = q
		}
	}
	return best > 0
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

// acceptMatches reports whether a media range admits the wanted type/subtype.
func acceptMatches(rng string, want []string) bool {
	if rng == "*/*" {
		return true
	}
	rp := strings.SplitN(rng, "/", 2)
	if len(rp) != 2 {
		return false
	}
	if rp[0] != want[0] {
		return false
	}
	return rp[1] == "*" || rp[1] == want[1]
}
