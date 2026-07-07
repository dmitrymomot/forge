// Package requestlog emits one structured access-log line per request.
//
//	log, err := logger.New(logger.WithContextExtractors(requestid.LogExtractor, clientip.LogExtractor))
//	h := requestlog.New(log, requestlog.WithSkip(func(r *http.Request) bool { return r.URL.Path == "/healthz" }))(mux)
//
// The line carries method, path, status, duration, and bytes; request_id and
// client_ip arrive via the logger's extractors, not requestlog itself.
//
// Install requestlog INSIDE recoverer (recoverer wraps requestlog). A panicking request is
// logged by recoverer's panic line (with method/path), not by requestlog's access line,
// since the panic unwinds past requestlog before it can record the response.
package requestlog
