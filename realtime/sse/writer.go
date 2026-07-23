package sse

import (
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Writer streams Server-Sent Events over one HTTP response. NewWriter sets
// the stream headers and commits the response; every Send writes one frame
// and flushes it to the client. Safe for concurrent use.
type Writer struct {
	w           http.ResponseWriter
	rc          *http.ResponseController
	buf         []byte
	sendTimeout time.Duration
	mu          sync.Mutex
}

// NewWriter starts a Server-Sent Events stream on w: it sets the stream
// headers (Content-Type: text/event-stream, no-cache, proxy-buffering off),
// clears the server's write deadline where supported so the stream outlives
// httpserver's WriteTimeout — each Send then arms its own deadline (see
// WithSendTimeout) so a stalled client cannot pin the connection forever —
// and commits the 200 response by flushing the headers. If w cannot flush —
// required for events to reach the client as they happen — it resets the
// headers and returns ErrStreamingUnsupported before anything is written, so
// the caller can still respond with an error.
func NewWriter(w http.ResponseWriter, opts ...WriterOption) (*Writer, error) {
	c := &writerConfig{sendTimeout: defaultSendTimeout}
	for _, opt := range opts {
		opt(c)
	}
	if len(c.errs) > 0 {
		return nil, errors.Join(c.errs...)
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	// On any failure below, nothing has been written yet: reset the stream
	// headers so the caller can still respond with a clean error.
	resetHeaders := func() {
		h.Del("Content-Type")
		h.Del("Cache-Control")
		h.Del("X-Accel-Buffering")
	}
	rc := http.NewResponseController(w)
	if err := rc.SetWriteDeadline(time.Time{}); err != nil {
		if !errors.Is(err, http.ErrNotSupported) {
			resetHeaders()
			return nil, fmt.Errorf("sse: clear write deadline: %w", err)
		}
		// Deadline control is unavailable — a flushable middleware in the chain
		// does not implement Unwrap. Per-send deadlines then cannot be armed, so
		// a configured send timeout would be silently unenforced and a stalled
		// client could pin the connection forever. Fail closed before committing
		// unless the caller explicitly accepted unbounded writes with
		// WithSendTimeout(0). A writer that also cannot flush is a more
		// fundamental problem, reported as ErrStreamingUnsupported below.
		if c.sendTimeout > 0 && canFlush(w) {
			resetHeaders()
			return nil, fmt.Errorf("sse: response writer lacks the deadline control the per-send timeout needs; make wrappers implement Unwrap, or pass WithSendTimeout(0): %w", err)
		}
	}
	if err := rc.Flush(); err != nil {
		resetHeaders()
		if errors.Is(err, http.ErrNotSupported) {
			return nil, ErrStreamingUnsupported
		}
		return nil, fmt.Errorf("sse: flush headers: %w", err)
	}
	return &Writer{w: w, rc: rc, sendTimeout: c.sendTimeout}, nil
}

// Send frames e, writes it, and flushes it to the client. A validation
// failure returns ErrInvalidEvent with nothing written; a write or flush
// error means the client is gone (or stopped reading past WithSendTimeout)
// and the stream is over.
func (w *Writer) Send(e Event) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	buf, err := e.appendTo(w.buf[:0])
	if err != nil {
		return err
	}
	w.buf = buf
	if w.sendTimeout > 0 {
		// Best-effort: a chain that cannot set deadlines falls back to the
		// server's own write timeout.
		_ = w.rc.SetWriteDeadline(time.Now().Add(w.sendTimeout))
	}
	if _, err := w.w.Write(buf); err != nil {
		return fmt.Errorf("sse: write event: %w", err)
	}
	if err := w.rc.Flush(); err != nil {
		return fmt.Errorf("sse: flush event: %w", err)
	}
	return nil
}

// canFlush reports whether w supports flushing, mirroring how
// http.ResponseController resolves Flush along the Unwrap chain. It lets
// NewWriter tell a flushable-but-deadline-less writer (fail closed on the send
// timeout) apart from one that cannot stream at all (ErrStreamingUnsupported).
func canFlush(w http.ResponseWriter) bool {
	for {
		switch t := w.(type) {
		case interface{ FlushError() error }:
			return true
		case http.Flusher:
			return true
		case interface{ Unwrap() http.ResponseWriter }:
			w = t.Unwrap()
		default:
			return false
		}
	}
}
