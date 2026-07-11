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
		if !strings.Contains(accept, encodingToken(enc)) && !strings.Contains(accept, enc) {
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
		if inm := r.Header.Get("If-None-Match"); inm != "" && h.Get("Etag") != "" && strings.Contains(inm, h.Get("Etag")) {
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
