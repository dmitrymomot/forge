package assets

import (
	"io/fs"
	"net/http"
	"strconv"
	"strings"
)

// serveCompressed serves a precompressed sibling of name when precompression is
// enabled and the client accepts an encoding whose sibling exists. It returns
// true when it wrote the response. Range is intentionally unsupported for
// compressed responses; the caller has already set Content-Type / Cache-Control
// / Etag. Vary: Accept-Encoding is added whenever precompression is enabled.
func (a *Assets) serveCompressed(w http.ResponseWriter, r *http.Request, name string) bool {
	if len(a.precompress) == 0 {
		return false
	}
	h := w.Header()
	h.Add("Vary", "Accept-Encoding")
	if h.Get("Content-Type") == "" {
		// Unknown extension: the caller couldn't set a Content-Type. Serving
		// compressed bytes directly would let net/http sniff the COMPRESSED
		// stream (→ application/octet-stream) alongside Content-Encoding.
		// Fall through to identity serving, which sniffs the real bytes via
		// http.ServeContent.
		return false
	}
	accept := r.Header.Get("Accept-Encoding")
	for _, enc := range a.precompress {
		if !acceptsEncoding(accept, encodingToken(enc)) {
			continue
		}
		sibling := name + "." + enc
		data, err := fs.ReadFile(a.fsys, sibling)
		if err != nil {
			continue
		}
		h.Set("Content-Encoding", encodingToken(enc))
		h.Del("Accept-Ranges")
		// A strong validator must be unique per representation (RFC 9110
		// §8.8.3); Content-Encoding changes the representation, so the
		// compressed body gets its own Etag derived from the identity one.
		identity := strings.Trim(h.Get("Etag"), `"`)
		h.Set("Etag", strconv.Quote(identity+"."+enc))
		if inm := r.Header.Get("If-None-Match"); inm != "" && etagMatches(inm, h.Get("Etag")) {
			w.WriteHeader(http.StatusNotModified)
			return true
		}
		h.Set("Content-Length", strconv.Itoa(len(data)))
		w.WriteHeader(http.StatusOK)
		if r.Method != http.MethodHead {
			_, _ = w.Write(data)
		}
		return true
	}
	return false
}

// encodingToken maps a sibling extension to its Content-Encoding token.
func encodingToken(enc string) string {
	if enc == "gz" {
		return "gzip"
	}
	return enc // "br", "zstd", …
}

// acceptsEncoding reports whether the Accept-Encoding header accepts the given
// content-coding token (e.g. "br", "gzip") with a non-zero q-value. An explicitly
// listed token takes precedence; otherwise a wildcard "*" applies (RFC 9110
// §12.5.3), matching any coding not explicitly named.
func acceptsEncoding(header, token string) bool {
	starQ, hasStar := 0.0, false
	for part := range strings.SplitSeq(header, ",") {
		coding, params, _ := strings.Cut(part, ";")
		coding = strings.TrimSpace(coding)
		switch {
		case strings.EqualFold(coding, token):
			return codingQ(params) > 0
		case coding == "*":
			starQ, hasStar = codingQ(params), true
		}
	}
	return hasStar && starQ > 0
}

// codingQ returns the q-value of an Accept-Encoding directive's params: 1 when no
// "q=" is present (default accept), the parsed value when present and valid, or a
// negative sentinel when the q-value is present but malformed (treated as refusal).
func codingQ(params string) float64 {
	q, hasQ := strings.CutPrefix(strings.TrimSpace(params), "q=")
	if !hasQ {
		return 1
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(q), 64)
	if err != nil {
		return -1
	}
	return v
}

// etagMatches reports whether the If-None-Match header value matches etag.
// "*" matches any representation (RFC 9110 §13.1.2); otherwise the
// comma-separated list is compared entity-tag by entity-tag, ignoring a
// weak-prefix ("W/") since these validators are only ever compared here.
func etagMatches(inm, etag string) bool {
	inm = strings.TrimSpace(inm)
	if inm == "*" {
		return true
	}
	if etag == "" {
		return false
	}
	for tag := range strings.SplitSeq(inm, ",") {
		tag = strings.TrimSpace(tag)
		tag = strings.TrimPrefix(tag, "W/")
		if tag == etag {
			return true
		}
	}
	return false
}
