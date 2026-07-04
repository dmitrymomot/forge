// Package reqlog emits one structured access-log line per request.
//
//	log, err := logger.New(logger.WithContextExtractors(requestid.LogExtractor, clientip.LogExtractor))
//	h := reqlog.New(log, reqlog.WithSkip(func(r *http.Request) bool { return r.URL.Path == "/healthz" }))(mux)
//
// The line carries method, path, status, duration, and bytes; request_id and
// client_ip arrive via the logger's extractors, not reqlog itself.
//
// Install reqlog INSIDE recoverer (recoverer wraps reqlog). A panicking request is
// logged by recoverer's panic line (with method/path), not by reqlog's access line,
// since the panic unwinds past reqlog before it can record the response.
package reqlog
