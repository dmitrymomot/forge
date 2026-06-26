package render

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"
	"sync"
)

const contentTypeJSON = "application/json; charset=utf-8"
const contentTypeHTML = "text/html; charset=utf-8"
const contentTypeText = "text/plain; charset=utf-8"
const contentTypeCSV = "text/csv; charset=utf-8"
const contentTypeOctet = "application/octet-stream"

// setContentType sets the Content-Type header to ct only when the caller has not
// already set one, so a handler can pre-set a custom charset/parameters and win.
func setContentType(w http.ResponseWriter, ct string) {
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", ct)
	}
}

// bufPool backs the transactional encoders (JSON, HTML, Templ).
var bufPool = sync.Pool{New: func() any { return new(bytes.Buffer) }}

func getBuf() *bytes.Buffer {
	if b, ok := bufPool.Get().(*bytes.Buffer); ok {
		return b
	}
	return new(bytes.Buffer)
}

func putBuf(b *bytes.Buffer) {
	const maxReuse = 1 << 20 // 1 MiB; don't pin large buffers in the pool
	if b.Cap() > maxReuse {
		return
	}
	b.Reset()
	bufPool.Put(b)
}

// contentDisposition builds a Content-Disposition header value for a download, e.g.
//
//	attachment; filename="report.csv"; filename*=UTF-8''r%C3%A9sum%C3%A9.csv
//
// It uses only the base name of filename, builds an injection-safe quoted ASCII
// fallback (control/quote/backslash/non-ASCII bytes replaced with '_'), and appends
// an RFC 5987 filename* with the exact UTF-8 name percent-encoded. When the name
// reduces to empty, only the disposition type is returned.
func contentDisposition(disposition, filename string) string {
	name := baseName(filename)
	if name == "" {
		return disposition
	}
	var ascii strings.Builder
	ascii.Grow(len(name))
	for i := range len(name) {
		c := name[i]
		if c < 0x20 || c == 0x7f || c == '"' || c == '\\' || c >= 0x80 {
			ascii.WriteByte('_')
		} else {
			ascii.WriteByte(c)
		}
	}
	return fmt.Sprintf("%s; filename=%q; filename*=UTF-8''%s",
		disposition, ascii.String(), rfc5987Encode(name))
}

// baseName returns the final path element, splitting on both '/' and '\' so a
// Windows-style or POSIX path cannot inject directory components.
func baseName(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[i+1:]
	}
	return p
}

// rfc5987Encode percent-encodes s per RFC 5987 ext-value (UTF-8), leaving only the
// attr-char set unescaped.
func rfc5987Encode(s string) string {
	const upperhex = "0123456789ABCDEF"
	var b strings.Builder
	b.Grow(len(s))
	for i := range len(s) {
		c := s[i]
		if isAttrChar(c) {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(upperhex[c>>4])
		b.WriteByte(upperhex[c&0x0f])
	}
	return b.String()
}

// isAttrChar reports whether c is in the RFC 5987 attr-char set.
func isAttrChar(c byte) bool {
	switch {
	case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		return true
	}
	switch c {
	case '!', '#', '$', '&', '+', '-', '.', '^', '_', '`', '|', '~':
		return true
	}
	return false
}
