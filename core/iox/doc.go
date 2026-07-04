// Package iox provides small io.Reader/Writer/Closer shims that streaming code
// reuses: a LimitReader that errors (rather than silently truncates) past its
// cap so callers can answer 413, a Drain+Close for keep-alive reuse, a
// MultiCloser, a CountingWriter, and a NopWriteCloser. It does not duplicate
// bufio or re-wrap stdlib TeeReader/io.NopCloser.
//
// # Usage
//
//	body := iox.LimitReader(r.Body, maxBodySize)
//	data, err := io.ReadAll(body)
//	if errors.Is(err, iox.ErrLimitExceeded) {
//		_ = iox.DrainClose(r.Body) // reject but keep the connection alive
//		return errTooLarge
//	}
package iox
