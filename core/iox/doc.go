// Package iox provides small io.Reader/Writer/Closer shims that streaming code
// reuses: a LimitReader that errors (rather than silently truncates) past its
// cap so callers can answer 413, a Drain+Close for keep-alive reuse, a
// MultiCloser, a CountingWriter, and a NopWriteCloser. It does not duplicate
// bufio or re-wrap stdlib TeeReader/io.NopCloser.
package iox
