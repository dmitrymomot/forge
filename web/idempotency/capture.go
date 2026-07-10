package idempotency

import (
	"bytes"
	"net/http"
)

// capture buffers a handler's response so the middleware can decide whether to
// store it before flushing to the client. Header() passes through to the wrapped
// writer, so headers stay uncommitted until flush. If the body exceeds limit it
// flips to overflow mode: the buffered bytes are streamed out and nothing is
// cached.
type capture struct {
	http.ResponseWriter
	buf    bytes.Buffer
	limit  int64
	status int
	wrote  bool
	over   bool
}

func (c *capture) WriteHeader(status int) {
	if c.wrote {
		return
	}
	c.status = status
	c.wrote = true
}

func (c *capture) Write(p []byte) (int, error) {
	if !c.wrote {
		c.WriteHeader(http.StatusOK)
	}
	if c.over {
		return c.ResponseWriter.Write(p)
	}
	if int64(c.buf.Len())+int64(len(p)) > c.limit {
		c.over = true
		c.ResponseWriter.WriteHeader(c.status)
		if c.buf.Len() > 0 {
			if _, err := c.ResponseWriter.Write(c.buf.Bytes()); err != nil {
				c.buf.Reset()
				return 0, err
			}
			c.buf.Reset()
		}
		return c.ResponseWriter.Write(p)
	}
	return c.buf.Write(p)
}

// finalStatus reports the status the handler set, defaulting to 200 if it wrote
// nothing.
func (c *capture) finalStatus() int {
	if !c.wrote {
		return http.StatusOK
	}
	return c.status
}

// flush writes the buffered response to the underlying writer. It is a no-op in
// overflow mode, where the response has already been streamed.
func (c *capture) flush() {
	if c.over {
		return
	}
	if !c.wrote {
		c.status = http.StatusOK
	}
	c.ResponseWriter.WriteHeader(c.status)
	if c.buf.Len() > 0 {
		_, _ = c.ResponseWriter.Write(c.buf.Bytes())
	}
}

// Flush implements http.Flusher. Because capture buffers to decide whether to
// store the response, flushing is incompatible with buffering: it flips capture
// into overflow mode (streaming the buffered bytes and everything after straight
// to the client, caching nothing), then flushes the underlying writer.
//
// Flush is the only http.ResponseController capability supported through the
// buffer: it degrades to uncached streaming. Hijack and write-deadline control
// can't work coherently against a buffered response, so capture deliberately
// does not expose Unwrap — http.ResponseController reports ErrNotSupported for
// them rather than corrupting the cache with a phantom response.
func (c *capture) Flush() {
	if !c.over {
		if !c.wrote {
			c.WriteHeader(http.StatusOK)
		}
		c.over = true
		c.ResponseWriter.WriteHeader(c.status)
		if c.buf.Len() > 0 {
			_, _ = c.ResponseWriter.Write(c.buf.Bytes())
			c.buf.Reset()
		}
	}
	if f, ok := c.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
