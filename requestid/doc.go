// Package requestid attaches a per-request correlation ID.
//
//	h := requestid.New()(mux)
//	logger.New(logger.WithContextExtractors(requestid.LogExtractor))
//
// A valid inbound X-Request-ID (printable ASCII, <=128 bytes) is trusted by
// default; otherwise a ULID is generated. The value is echoed on the response and
// available via requestid.From(ctx).
package requestid
