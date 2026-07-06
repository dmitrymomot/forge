package compress

import (
	"compress/flate"
	"compress/gzip"
	"io"
	"net/http"
	"strings"
	"sync"
)

// zwriter is the common surface of *gzip.Writer and *flate.Writer.
type zwriter interface {
	io.Writer
	Flush() error
	Close() error
	Reset(io.Writer)
}

// writer buffers up to minSize bytes before deciding whether to compress,
// so headers (Content-Encoding, Content-Length) and the status line go out
// correct. Flush forces an immediate decision — SSE streams compress from
// the first event.
type writer struct {
	rw      http.ResponseWriter
	zw      zwriter
	rc      *http.ResponseController
	pool    *sync.Pool
	enc     string
	types   []string
	buf     []byte
	minSize int
	status  int
	decided bool
}

func (w *writer) Header() http.Header { return w.rw.Header() }

func (w *writer) WriteHeader(code int) {
	if w.decided {
		return
	}
	if w.status == 0 {
		w.status = code
	}
}

func (w *writer) Write(p []byte) (int, error) {
	if !w.decided {
		w.buf = append(w.buf, p...)
		if len(w.buf) >= w.minSize {
			if err := w.decide(false); err != nil {
				return 0, err
			}
		}
		return len(p), nil
	}
	if w.zw != nil {
		return w.zw.Write(p)
	}
	return w.rw.Write(p)
}

// Flush decides immediately (streaming) and flushes through to the client.
func (w *writer) Flush() {
	_ = w.decide(false)
	if w.zw != nil {
		_ = w.zw.Flush()
	}
	_ = w.rc.Flush()
}

// Unwrap exposes the underlying writer for http.ResponseController.
func (w *writer) Unwrap() http.ResponseWriter { return w.rw }

// decide picks compressed vs plain, sends the delayed status + buffer, and
// locks the choice in. final=true means the body is complete (close path).
func (w *writer) decide(final bool) error {
	if w.decided {
		return nil
	}
	w.decided = true
	h := w.rw.Header()
	ct := h.Get("Content-Type")
	if ct == "" && len(w.buf) > 0 {
		ct = http.DetectContentType(w.buf)
		h.Set("Content-Type", ct)
	}
	//nolint:staticcheck // De Morgan's form obscures the "skip compression" intent; keep negated as written.
	compressing := h.Get("Content-Encoding") == "" &&
		typeAllowed(w.types, ct) &&
		!(final && len(w.buf) < w.minSize)
	if compressing {
		h.Set("Content-Encoding", w.enc)
		h.Del("Content-Length")
		zw := w.pool.Get().(zwriter)
		zw.Reset(w.rw)
		w.zw = zw
	}
	if w.status == 0 {
		w.status = http.StatusOK
	}
	w.rw.WriteHeader(w.status)
	if len(w.buf) > 0 {
		var err error
		if w.zw != nil {
			_, err = w.zw.Write(w.buf)
		} else {
			_, err = w.rw.Write(w.buf)
		}
		w.buf = nil
		return err
	}
	return nil
}

// close finalizes the response: undecided buffers flush as-is (plain when
// under MinSize), compressed streams close and return their writer to the
// pool.
func (w *writer) close() {
	// A handler that wrote nothing at all (no explicit status, no body) must
	// not cause us to commit a header. Committing here would flip an outer
	// middleware's "response already written" flag (e.g. web/timeout's
	// !Wrote() check), suppressing its own status such as 504. Leaving the
	// response uncommitted lets the outer layer — or the server's implicit
	// 200 — decide.
	if !w.decided && w.status == 0 && len(w.buf) == 0 {
		return
	}
	_ = w.decide(true)
	if w.zw != nil {
		_ = w.zw.Close()
		w.pool.Put(w.zw)
		w.zw = nil
	}
}

func typeAllowed(allowed []string, ct string) bool {
	ct, _, _ = strings.Cut(ct, ";")
	ct = strings.TrimSpace(strings.ToLower(ct))
	for _, a := range allowed {
		if base, ok := strings.CutSuffix(a, "/*"); ok {
			if strings.HasPrefix(ct, base+"/") {
				return true
			}
			continue
		}
		if ct == a {
			return true
		}
	}
	return false
}

func newPool(enc string, level int) *sync.Pool {
	return &sync.Pool{New: func() any {
		if enc == "deflate" {
			zw, _ := flate.NewWriter(io.Discard, level)
			return zwriter(zw)
		}
		zw, _ := gzip.NewWriterLevel(io.Discard, level)
		return zwriter(zw)
	}}
}
