// Package problem maps errors to HTTP error responses.
//
// The Responder seam is what error-writing middleware accepts:
//
//	type Responder func(w http.ResponseWriter, r *http.Request, err error)
//
// Shipped responders — none ships markup or leaks the error text on 5xx:
//   - JSON: application/problem+json (RFC 9457)
//   - Text: text/plain via text/template
//   - HTML: text/html via a caller-supplied html/template
//   - Component: text/html via a render.Component (templ)
//
// Negotiate dispatches responders by the Accept header with a fallback:
//
//	recoverer.New(recoverer.WithResponder(
//		problem.Negotiate(problem.JSON(), map[string]problem.Responder{
//			"text/html": problem.Component(errorPage),
//		})))
//
// From maps an error to a Problem document without writing.
package problem
