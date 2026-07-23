package session

import (
	"bufio"
	"errors"
	"net"
	"net/http"
)

// commitWriter runs the session commit exactly once, immediately before the
// first byte reaches the real writer. Headers are still open at that moment,
// so a failed commit becomes a clean 500 with nothing leaked.
//
// It re-declares Hijack and FlushError on purpose. http.ResponseController
// walks the Unwrap chain, so a websocket upgrade or an SSE flush would
// otherwise reach the real writer without ever passing through WriteHeader,
// skipping the commit entirely.
type commitWriter struct {
	http.ResponseWriter
	commit    func() error
	committed bool
	failed    bool
}

func newCommitWriter(w http.ResponseWriter, commit func() error) *commitWriter {
	return &commitWriter{ResponseWriter: w, commit: commit}
}

// ensureCommitted runs the commit once. It reports whether writing may proceed.
func (w *commitWriter) ensureCommitted() error {
	if w.committed {
		if w.failed {
			return errors.New("session: commit already failed")
		}
		return nil
	}
	w.committed = true
	if err := w.commit(); err != nil {
		w.failed = true
		return err
	}
	return nil
}

// WriteHeader commits before the status line goes out.
func (w *commitWriter) WriteHeader(code int) {
	if !w.committed {
		if err := w.ensureCommitted(); err != nil {
			clearHeaders(w.Header())
			w.ResponseWriter.WriteHeader(http.StatusInternalServerError)
			return
		}
	}
	if w.failed {
		return
	}
	w.ResponseWriter.WriteHeader(code)
}

// Write commits on an implicit 200 and swallows the body once a commit failed.
func (w *commitWriter) Write(b []byte) (int, error) {
	if !w.committed {
		w.WriteHeader(http.StatusOK)
	}
	if w.failed {
		// Report success so the handler proceeds normally; the client already
		// received a 500 and must not also receive the handler's payload.
		return len(b), nil
	}
	return w.ResponseWriter.Write(b)
}

// Unwrap exposes the wrapped writer to http.ResponseController.
func (w *commitWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// Hijack commits before the connection leaves the HTTP stack.
func (w *commitWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if err := w.ensureCommitted(); err != nil {
		return nil, nil, err
	}
	return http.NewResponseController(w.ResponseWriter).Hijack()
}

// FlushError commits before the first flush, which ResponseController would
// otherwise route straight past WriteHeader.
func (w *commitWriter) FlushError() error {
	if !w.committed {
		if err := w.ensureCommitted(); err != nil {
			clearHeaders(w.Header())
			w.ResponseWriter.WriteHeader(http.StatusInternalServerError)
			return err
		}
	}
	return http.NewResponseController(w.ResponseWriter).Flush()
}

func clearHeaders(h http.Header) {
	for k := range h {
		h.Del(k)
	}
}
