// Package pagination derives bounded SQLC-ready pagination values from an HTTP request. Parse reads the 1-based "page" and "per_page" query parameters, clamps their numeric values to safe bounds, and returns the Limit and Offset a static list query needs. ParseCursor reads an opaque "cursor" and "limit" into similarly bounded values for a static cursor query.
//
// The helpers are free functions. They do not build SQL, execute queries, encode or decode cursors, count rows, or render navigation: those concerns belong to the consumer's static sqlc query and response type. A malformed present query parameter returns a *request.Error, so request.StatusCode maps it to HTTP 400.
//
// The default offset input is page=1 and per_page=20; the default cursor input is cursor="" and limit=20. Both limits are clamped to 1..100. Use WithPageParams or WithCursorParams when an endpoint has different parameter names, and WithDefaultLimit or WithMaxLimit to change the bounds.
//
// # Usage
//
//	p, err := pagination.Parse(r)
//	if err != nil {
//		render.JSON(w, request.StatusCode(err), apiError{err.Error()})
//		return
//	}
//	rows, err := queries.ListUsers(r.Context(), db.ListUsersParams{Limit: p.Limit, Offset: p.Offset})
package pagination
