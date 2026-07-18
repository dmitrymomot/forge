package iox

import (
	"errors"
	"io"
)

// LimitReader returns a reader that yields at most n bytes from r and then
// returns ErrLimitExceeded. Unlike io.LimitReader (which reports io.EOF at the
// cap), it distinguishes "hit the limit" from "clean end of input".
//
// A negative n is treated as 0 (any input immediately exceeds the limit), so a
// misconfigured or negative body-size limit fails cleanly with ErrLimitExceeded
// rather than panicking or returning a negative byte count from Read.
func LimitReader(r io.Reader, n int64) io.Reader {
	if n < 0 {
		n = 0
	}
	return &limitReader{r: r, n: n}
}

type limitReader struct {
	r   io.Reader
	err error
	n   int64 // bytes still allowed
}

func (l *limitReader) Read(p []byte) (int, error) {
	if l.err != nil {
		return 0, l.err
	}
	if len(p) == 0 {
		return 0, nil
	}
	// Read at most one byte past the limit so we can detect an overrun.
	if int64(len(p)) > l.n+1 {
		p = p[:l.n+1]
	}
	n, err := l.r.Read(p)
	if int64(n) <= l.n {
		l.n -= int64(n)
		l.err = err
		return n, err
	}
	// Underlying reader produced a byte beyond the limit.
	l.err = ErrLimitExceeded
	return int(l.n), l.err
}

// DrainClose discards any remaining bytes then closes rc, letting an HTTP
// client reuse the keep-alive connection. It returns the copy error if any,
// otherwise the close error.
func DrainClose(rc io.ReadCloser) error {
	_, copyErr := io.Copy(io.Discard, rc)
	closeErr := rc.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

// MultiCloser returns an io.Closer that closes every closer (skipping nils),
// aggregating failures with errors.Join.
func MultiCloser(closers ...io.Closer) io.Closer {
	return multiCloser(closers)
}

type multiCloser []io.Closer

func (m multiCloser) Close() error {
	var errs []error
	for _, c := range m {
		if c == nil {
			continue
		}
		if err := c.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// CountingWriter wraps an io.Writer and counts the bytes written through it.
type CountingWriter struct {
	w io.Writer
	n int64
}

// NewCountingWriter returns a CountingWriter delegating to w.
func NewCountingWriter(w io.Writer) *CountingWriter {
	return &CountingWriter{w: w}
}

// Write writes p to the underlying writer and adds the bytes actually
// written to the running count, even on a short write or error.
func (c *CountingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

// N returns the total number of bytes written so far.
func (c *CountingWriter) N() int64 { return c.n }

// NopWriteCloser wraps w with a no-op Close.
func NopWriteCloser(w io.Writer) io.WriteCloser {
	return nopWriteCloser{w}
}

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }
