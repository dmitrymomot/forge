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
	w.Header().Add("Vary", "Accept-Encoding")
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
		h := w.Header()
		h.Set("Content-Encoding", encodingToken(enc))
		h.Del("Accept-Ranges")
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
