// Package recoverer is the outermost middleware: it turns a handler panic into a
// 500 response and an Error log line.
//
// The recovered value is wrapped in ErrPanic and passed to the responder. If the
// handler already committed a response, recoverer only logs. http.ErrAbortHandler
// propagates unchanged.
//
// # Usage
//
//	h := middleware.Wrap(mux, recoverer.New()) // defaults to problem.JSON() forced to HTTP 500
package recoverer
