// Package respond turns what a handler decided into bytes, without the handler
// writing anything.
//
//	type Handler func(r *http.Request) (Response, error)
//
// A handler that writes decides two things at once: what the answer is, and how it
// reaches this client. Only the first is the handler's business. The second differs
// per client — htmx follows no 303, so every redirect grows a branch — and per
// router tree, since a 404 is a page on the public site and a problem document on
// the API. A handler that writes can also write twice, or write and then fail, and
// the compiler says nothing.
//
// Here the answer stays a value until the edge writes it, and the edge is where the
// client and the tree are known.
//
// # Usage
//
//	func payInvoice(r *http.Request) (respond.Response, error) {
//		if err := billing.Pay(r.Context(), r.PathValue("id")); err != nil {
//			return nil, err
//		}
//		return respond.SeeOther("/invoices", respond.WithBefore(
//			flash.Setter(flashes, r, flash.Success("the invoice is paid")))), nil
//	}
//
//	pages := respond.New(respond.WithProblem(problem.Component(errorPage)))
//	mux.Handle("POST /invoices/{id}/pay", pages.Wrap(payInvoice))
//
// The concrete responses are Text, JSON, HTML, Templ, Components, Blob, Stream,
// Attachment, CSV, SeeOther, Found, External, File, FileFS, NoContent, and Raw. Each
// takes options rather than growing fields: WithStatus, WithHeader, WithAddedHeader,
// and WithBefore apply to all of them.
//
// SeeOther speaks the client's language — htmx receives 200 and HX-Redirect, a
// browser receives 303 and Location — so that branch exists once instead of in every
// handler that redirects.
//
// WithBefore is for a side effect that must land before the status is committed. A
// flash cookie is the case it exists for: a redirect's headers are gone the moment
// the status is written, so the cookie and the redirect must stay in step. A failing
// side effect fails the whole response rather than silently losing the message.
//
// # One Responder per router tree
//
// A Responder holds the problem responder that renders every refusal of its tree, so
// the dialect is a wiring decision and no handler reads an Accept header. Give the
// page tree problem.Component or problem.HTML and the API tree problem.JSON; mount
// NotFound and MethodNotAllowed from the same Responder and a missed route answers
// in the tree's dialect too. A new tree is a second Responder and nothing else.
//
// Fail is exported so middleware that owns its own status — a deadline, a refused
// token — answers in the same dialect as the handlers it wraps.
//
// # Raw
//
// A handler that must own the writer reaches for Raw: a hijack, an SSE loop, a body
// with no shape here. It is deliberately more ceremony than the rest, so a reader
// sees where the value-returning contract stops.
//
// # Cost
//
// Returning the answer as a value instead of writing it costs about 37ns and two
// allocations per request (BenchmarkWrapText against BenchmarkRenderTextDirect):
// the response struct is boxed into the interface. Call render directly on a route
// where that matters — the two compose, and a Responder tree can mix both.
package respond
