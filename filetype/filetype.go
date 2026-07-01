package filetype

import (
	"net/http"
	"strings"
)

// Type is a detected file type: its canonical MIME string and a conventional
// file extension (without a leading dot).
type Type struct {
	MIME string
	Ext  string
}

// octetStream is the MIME net/http.DetectContentType returns when it cannot
// recognize the content — treated here as "genuinely unknown".
const octetStream = "application/octet-stream"

// Detect matches head against the curated signature table; on a miss it falls
// back to net/http.DetectContentType. ok is false only when even the fallback
// yields application/octet-stream (i.e. the content is genuinely unrecognized),
// in which case Type.MIME is application/octet-stream and Type.Ext is empty.
func Detect(head []byte) (Type, bool) {
	// Empty input is genuinely unknown. net/http.DetectContentType reports
	// text/plain for zero bytes, so guard here to keep "no content" ⇒ ok=false.
	if len(head) == 0 {
		return Type{MIME: octetStream}, false
	}
	for _, sig := range signatures {
		if sig.match(head) {
			return sig.typ, true
		}
	}
	mime := http.DetectContentType(head)
	if i := strings.IndexByte(mime, ';'); i >= 0 {
		mime = strings.TrimSpace(mime[:i])
	}
	if mime == octetStream {
		return Type{MIME: octetStream}, false
	}
	return Type{MIME: mime}, true
}

// Is reports whether head's detected type has exactly the given MIME. A head
// that Detect cannot recognize (ok=false) never matches, so Is(head,
// "application/octet-stream") is false for unknown content.
func Is(head []byte, mime string) bool {
	t, ok := Detect(head)
	return ok && t.MIME == mime
}
