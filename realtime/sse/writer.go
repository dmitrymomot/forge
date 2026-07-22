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
	rc := http.NewResponseController(w)
	// A wrapping middleware that does not implement Unwrap surfaces
	// ErrNotSupported here even though the underlying connection has a
	// deadline; that is the caller's cue to run the server with
	// WriteTimeout=0 instead (see the package comment).
	if err := rc.SetWriteDeadline(time.Time{}); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return nil, fmt.Errorf("sse: clear write deadline: %w", err)
	}
	if err := rc.Flush(); err != nil {
		h.Del("Content-Type")
		h.Del("Cache-Control")
		h.Del("X-Accel-Buffering")
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
