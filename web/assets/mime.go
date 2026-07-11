package assets

import (
	"mime"
	"path"
	"strings"
)

// mimeOverlay corrects or fills content types stdlib mime gets wrong or misses.
var mimeOverlay = map[string]string{
	".js":          "text/javascript; charset=utf-8",
	".mjs":         "text/javascript; charset=utf-8",
	".css":         "text/css; charset=utf-8",
	".json":        "application/json",
	".map":         "application/json",
	".woff2":       "font/woff2",
	".webmanifest": "application/manifest+json",
	".avif":        "image/avif",
	".wasm":        "application/wasm",
}

// contentType returns the content type for name, or "" to let ServeContent sniff.
func contentType(name string) string {
	ext := strings.ToLower(path.Ext(name))
	if ct, ok := mimeOverlay[ext]; ok {
		return ct
	}
	return mime.TypeByExtension(ext)
}
