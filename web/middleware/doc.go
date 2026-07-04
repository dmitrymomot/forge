// Package middleware defines forge's HTTP composition seam.
//
// A Middleware is a func(http.Handler) http.Handler. Compose middleware with
// Chain/Wrap and pass the result to httpserver.New — the first middleware is the
// outermost layer.
//
// WrapWriter records the response status and byte count for access logging and
// panic recovery; use http.ResponseController for flushing/hijacking.
//
// # Usage
//
//	h := middleware.Wrap(mux,
//		recoverer.New(),
//		requestid.New(),
//	)
//	srv := httpserver.New(h)
package middleware
