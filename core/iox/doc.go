// Package iox provides small io.Reader/Writer/Closer shims that streaming code
// reuses: a LimitReader that errors (rather than silently truncates) past its
// cap so callers can answer 413, a Drain+Close for keep-alive reuse, a
// MultiCloser, a CountingWriter, and a NopWriteCloser. It does not duplicate
// bufio or re-wrap stdlib TeeReader/io.NopCloser.
//
// # Usage
//
//	var body io.ReadCloser // from an *http.Request's Body
//	limited := iox.LimitReader(body, 1<<20)
//	data, err := io.ReadAll(limited)
//	if errors.Is(err, iox.ErrLimitExceeded) {
//		_ = iox.DrainClose(body) // reject but keep the connection alive
//	}
package iox
