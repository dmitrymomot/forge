package subroute

import (
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/dmitrymomot/forge/web/request"
)

// Mount registers h on mux at prefix, both as the exact path and as a
// subtree. prefix may contain single-segment {name} wildcards. Requests are
// dispatched to h with the prefix segments stripped from the URL path; a
// request for the bare prefix reaches h with path "/". Path values matched
// by the prefix are captured and readable inside h via request.Path.
//
// Mount panics on nil arguments or a malformed prefix; ServeMux.Handle
// additionally panics on pattern conflicts and invalid wildcard names.
func Mount(mux *http.ServeMux, prefix string, h http.Handler) {
	if mux == nil {
		panic("subroute: nil mux")
	}
	if h == nil {
		panic(fmt.Sprintf("subroute: nil handler for prefix %q", prefix))
	}
	segments, names := parsePrefix(prefix)
	sh := &stripHandler{h: h, names: names, segments: segments}
	mux.Handle(prefix, sh)
	mux.Handle(prefix+"/", sh)
}

// parsePrefix validates prefix and returns its segment count and wildcard
// names. Wildcard name syntax beyond structure (valid identifiers) is left
// to ServeMux.Handle, which panics with its own message.
func parsePrefix(prefix string) (segments int, names []string) {
	switch {
	case prefix == "" || prefix[0] != '/':
		panic(fmt.Sprintf("subroute: prefix %q must start with '/'", prefix))
	case prefix == "/":
		panic(`subroute: prefix "/" is not mountable; register the handler on the mux directly`)
	case strings.HasSuffix(prefix, "/"):
		panic(fmt.Sprintf("subroute: prefix %q must not end with '/'", prefix))
	case strings.ContainsAny(prefix, " \t"):
		panic(fmt.Sprintf("subroute: prefix %q must not contain whitespace", prefix))
	}
	for seg := range strings.SplitSeq(prefix[1:], "/") {
		segments++
		switch {
		case seg == "":
			panic(fmt.Sprintf("subroute: prefix %q contains an empty segment", prefix))
		case seg[0] == '{' && seg[len(seg)-1] == '}':
			name := seg[1 : len(seg)-1]
			switch {
			case name == "" || name == "$":
				panic(fmt.Sprintf("subroute: prefix %q: %q is not a valid wildcard", prefix, seg))
			case strings.HasSuffix(name, "..."):
				panic(fmt.Sprintf("subroute: prefix %q: multi-segment wildcard %q is not allowed", prefix, seg))
			case slices.Contains(names, name):
				panic(fmt.Sprintf("subroute: prefix %q: duplicate wildcard name %q", prefix, name))
			}
			names = append(names, name)
		case strings.ContainsAny(seg, "{}"):
			panic(fmt.Sprintf("subroute: prefix %q: wildcard must occupy a whole segment, got %q", prefix, seg))
		}
	}
	return segments, names
}

// stripHandler dispatches to h with the mount prefix removed from the URL of
// a cloned request. For wildcard prefixes (names non-nil) it first captures
// the prefix path values via request.WithPathValues.
type stripHandler struct {
	h        http.Handler
	names    []string
	segments int
}

func (s *stripHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var r2 *http.Request
	if len(s.names) > 0 {
		vals := make(map[string]string, len(s.names))
		for _, n := range s.names {
			vals[n] = r.PathValue(n)
		}
		// WithPathValues merges over enclosing captures (later wins), so
		// nested mounts accumulate params with the innermost mount winning.
		r2 = r.WithContext(request.WithPathValues(r.Context(), vals))
	} else {
		// Static prefix: no capture, no context allocation.
		r2 = new(http.Request)
		*r2 = *r
	}
	r2.URL = new(url.URL)
	*r2.URL = *r.URL

	// Strip on the escaped path so encoded separators (%2F) inside segments
	// survive; then derive the decoded Path, keeping the URL invariant that
	// RawPath is set only when it differs from Path.
	tail := stripSegments(r.URL.EscapedPath(), s.segments)
	if tail == "" {
		tail = "/" // bare-prefix match: mounted handler sees the root
	}
	p, err := url.PathUnescape(tail)
	if err != nil { // unreachable for paths ServeMux matched; keep escaped form
		p = tail
	}
	r2.URL.Path = p
	if p == tail {
		r2.URL.RawPath = ""
	} else {
		r2.URL.RawPath = tail
	}
	s.h.ServeHTTP(w, r2)
}

// stripSegments returns path without its first n segments, starting at the
// slash that follows them ("/a/b/c", 2 -> "/c"). It returns "" when path has
// exactly n segments. path must begin with '/', which ServeMux guarantees
// for matched requests.
func stripSegments(path string, n int) string {
	i := 0
	for range n {
		j := strings.IndexByte(path[i+1:], '/')
		if j < 0 {
			return ""
		}
		i += 1 + j
	}
	return path[i:]
}
