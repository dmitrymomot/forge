// Package request provides small, stateless, reflection-free helpers for reading
// data off an *http.Request into Go values: typed accessors for query, path,
// header, cookie, and form values (Query, Path, Header, Cookie, FormValue and
// their Func/Slice/Split variants), strict body decoding (DecodeJSON, RawBody,
// multipart File/Files), and focused readers (BearerToken, presence
// predicates).
//
// The helpers are free functions — no constructor, no binder, no struct tags, no
// global state. Each typed accessor returns the zero value of T and a nil error
// when the key is absent, and a *request.Error only when a present value fails to
// parse. request.StatusCode maps that error to the right HTTP status (400/413/415):
//
//	page, err := request.Query[int](r, "page", 1)
//	if err != nil {
//		render.JSON(w, request.StatusCode(err), apiErr{err.Error()})
//		return
//	}
//
// Custom types parse with no reflection: any type whose pointer implements
// encoding.TextUnmarshaler (uuid.UUID, netip.Addr, time.Time, custom enums) works
// through the generic engine; anything else can use the Func variants.
//
// Path accessors resolve the current mux match (r.PathValue) first, then
// values stored with WithPathValues — the seam web/subroute uses so mount-
// prefix params stay readable inside mounted handlers.
//
// request reads; the render package writes; the htmx package handles HX-* headers.
// None imports another. Stdlib only.
package request
