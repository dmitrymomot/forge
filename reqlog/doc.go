// Package reqlog emits one structured access-log line per request.
//
//	log := logger.New(logger.WithContextExtractors(requestid.LogExtractor, clientip.LogExtractor))
//	h := reqlog.New(log, reqlog.WithSkip(func(r *http.Request) bool { return r.URL.Path == "/healthz" }))(mux)
//
// The line carries method, path, status, duration, and bytes; request_id and
// client_ip arrive via the logger's extractors, not reqlog itself.
package reqlog
