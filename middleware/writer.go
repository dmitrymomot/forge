package middleware

import "net/http"

// ResponseWriter records the status code and body size written through it, and
// whether the header has been committed. It exposes the wrapped writer via Unwrap
// so http.ResponseController can reach optional interfaces (Flusher, Hijacker,
// SetWriteDeadline, ...) without this type re-declaring each one.
type ResponseWriter interface {
	http.ResponseWriter
	Status() int    // 0 until the first write; the WriteHeader code, or 200 on implicit write
	Written() int64 // body bytes written
	Wrote() bool    // has the header been committed?
	Unwrap() http.ResponseWriter
}

type recorder struct {
	http.ResponseWriter
	written int64
	status  int
	wrote   bool
}

// WrapWriter wraps w. If w is already a middleware.ResponseWriter it is returned
// unchanged, so re-wrapping in nested middleware is cheap and non-duplicating.
func WrapWriter(w http.ResponseWriter) ResponseWriter {
	if rw, ok := w.(ResponseWriter); ok {
		return rw
	}
	return &recorder{ResponseWriter: w}
}

func (r *recorder) WriteHeader(status int) {
	if r.wrote {
		return
	}
	r.status = status
	r.wrote = true
	r.ResponseWriter.WriteHeader(status)
}

func (r *recorder) Write(b []byte) (int, error) {
	if !r.wrote {
		r.WriteHeader(http.StatusOK)
	}
	n, err := r.ResponseWriter.Write(b)
	r.written += int64(n)
	return n, err
}

func (r *recorder) Status() int {
	if !r.wrote {
		return 0
	}
	return r.status
}

func (r *recorder) Written() int64              { return r.written }
func (r *recorder) Wrote() bool                 { return r.wrote }
func (r *recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }
