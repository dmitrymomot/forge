// Package compress is middleware that gzip/deflate-compresses responses
// negotiated from the request's Accept-Encoding header.
//
// gzip is preferred over deflate when both are equally acceptable (including
// the common "gzip, deflate" and "deflate, gzip" cases and outright ties on
// q-value). A q=0 for an encoding disables it, per RFC 9110. Requests with
// no usable Accept-Encoding, HEAD requests, Range requests, and Upgrade
// requests all pass through unchanged.
//
// # MinSize buffering
//
// The response is buffered up to Config.MinSize bytes before a decision is
// made. This lets the middleware inspect (or sniff) the effective
// Content-Type and skip compression entirely for small bodies, where the
// gzip/deflate framing overhead outweighs any savings. Once the buffer
// reaches MinSize, or the handler calls Flush, or the handler finishes
// writing, the decision locks in: Content-Encoding and Content-Length are
// set (or left alone) and the buffered bytes flush through, compressed or
// not. A response already carrying Content-Encoding (an upstream proxy or
// the handler itself already compressed or encoded the body) is never
// double-compressed. Only content types matching the allowlist (WithContentTypes,
// defaulting to text/*, application/json, application/javascript, and
// image/svg+xml) are compressed.
//
// # SSE and Flush
//
// Server-Sent Event handlers call http.Flusher.Flush after every event.
// This middleware's Flush forces the compress-or-not decision immediately
// (so the first event decides the stream), then drains the underlying
// compressor and flushes the wrapped http.ResponseWriter via
// http.NewResponseController — so each event reaches the client as soon as
// the handler flushes, instead of sitting in the compressor's internal
// buffer until the response closes.
//
// # Ordering
//
// Install compress OUTSIDE the application handler but INSIDE reqlog, so
// reqlog measures the actual (pre-compression) bytes written by the
// handler, not the compressed wire size:
//
//	h := middleware.Wrap(mux,
//		recoverer.New(),
//		reqlog.New(log),
//		compressMW,
//	)
//
// # Usage
//
//	mw, err := compress.New(compress.WithConfig(cfg))
//	if err != nil {
//		// invalid Level or negative MinSize
//	}
//	handler := middleware.Wrap(mux, mw)
package compress
