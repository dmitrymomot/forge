package internal

import (
	"bufio"
	"net"
	"net/http"
	"sync"
)

// ResponseWriter wraps http.ResponseWriter to provide response interception.
// It tracks write status, runs hooks before the first write, and transforms
// status codes for HTMX requests.
type ResponseWriter struct {
	http.ResponseWriter
	status      int
	size        int64
	written     bool
	isHTMX      bool
	beforeWrite []func()
	mu          sync.Mutex
}

// NewResponseWriter creates a new ResponseWriter.
func NewResponseWriter(w http.ResponseWriter, isHTMX bool) *ResponseWriter {
	return &ResponseWriter{
		ResponseWriter: w,
		status:         http.StatusOK,
		isHTMX:         isHTMX,
	}
}

// OnBeforeWrite registers a hook to run before the first write.
// Hooks are called in registration order when WriteHeader or Write is first called.
func (w *ResponseWriter) OnBeforeWrite(fn func()) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.beforeWrite = append(w.beforeWrite, fn)
}

// runHooks executes all beforeWrite hooks once.
func (w *ResponseWriter) runHooks() {
	for _, fn := range w.beforeWrite {
		fn()
	}
	w.beforeWrite = nil // Clear to prevent double execution
}

// WriteHeader sends an HTTP response header with the provided status code.
// For HTMX requests, non-200 status codes are transformed to 200.
func (w *ResponseWriter) WriteHeader(code int) {
	w.mu.Lock()
	if w.written {
		w.mu.Unlock()
		return
	}
	w.written = true
	w.status = code

	// Run hooks before writing
	hooks := w.beforeWrite
	w.beforeWrite = nil
	w.mu.Unlock()

	for _, fn := range hooks {
		fn()
	}

	// HTMX transformation: non-200 → 200
	if w.isHTMX && code != http.StatusOK {
		code = http.StatusOK
	}

	w.ResponseWriter.WriteHeader(code)
}

// Write writes the data to the connection as part of an HTTP reply.
func (w *ResponseWriter) Write(b []byte) (int, error) {
	w.mu.Lock()
	if !w.written {
		w.written = true
		hooks := w.beforeWrite
		w.beforeWrite = nil
		w.mu.Unlock()

		for _, fn := range hooks {
			fn()
		}

		// HTMX transformation for implicit 200
		w.ResponseWriter.WriteHeader(w.status)
	} else {
		w.mu.Unlock()
	}

	n, err := w.ResponseWriter.Write(b)
	w.size += int64(n)
	return n, err
}

// Status returns the HTTP status code of the response.
func (w *ResponseWriter) Status() int {
	return w.status
}

// Size returns the number of bytes written to the response body.
func (w *ResponseWriter) Size() int64 {
	return w.size
}

// Written returns true if the response has been written.
func (w *ResponseWriter) Written() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.written
}

// Flush implements the http.Flusher interface.
func (w *ResponseWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Hijack implements the http.Hijacker interface.
func (w *ResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hijacker, ok := w.ResponseWriter.(http.Hijacker); ok {
		return hijacker.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}

// Push implements the http.Pusher interface.
func (w *ResponseWriter) Push(target string, opts *http.PushOptions) error {
	if pusher, ok := w.ResponseWriter.(http.Pusher); ok {
		return pusher.Push(target, opts)
	}
	return http.ErrNotSupported
}

// Unwrap returns the underlying ResponseWriter.
// This allows middleware to access the original writer if needed.
func (w *ResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
