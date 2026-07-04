// Package requestid attaches a per-request correlation ID: a trusted inbound
// header value or a freshly generated ULID.
//
// The ID is echoed on the response header, stored in the request context for
// retrieval via From, and exposed to structured logging via LogExtractor.
//
// # Usage
//
//	mux := http.NewServeMux()
//	h := requestid.New()(mux)
//
//	log, err := logger.New(logger.WithContextExtractors(requestid.LogExtractor))
//
// A valid inbound X-Request-ID (printable ASCII, <=128 bytes) is trusted by
// default; otherwise a ULID is generated. The value is echoed on the response and
// available via requestid.From(ctx).
package requestid
