package render

import (
	"bytes"
	"net/http"
	"sync"
)

const contentTypeJSON = "application/json; charset=utf-8"
const contentTypeHTML = "text/html; charset=utf-8"

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
