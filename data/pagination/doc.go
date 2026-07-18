// Package pagination provides the two pagination models a SaaS backend needs,
// as composable pieces that emit SQL fragments rather than run queries.
//
// Keyset (cursor) pagination is the default for APIs, feeds, and infinite
// scroll: it is stable under inserts and O(1) at any depth. A Keyset names the
// ordered columns — the last unique, every column NOT NULL — and builds the
// portable OR-of-ANDs WHERE comparison and the ORDER BY list, with a
// placeholder-dialect option (Dollar "$n" for pgx, Question "?" for
// ClickHouse/MySQL/SQLite) and a caller-chosen start index so the fragment
// composes after a query's existing arguments (a tenant scope clause, say). A
// Codec turns the boundary row's key values into an opaque base64url(JSON)
// cursor, optionally HMAC-signed via crypto/sign so it is tamper-evident.
// NewPage assembles the fetched rows and the next/prev cursors into a Page[T],
// handling forward and backward paging.
//
// Offset (numbered) pagination serves server-rendered admin tables: PageCount
// and Offset drive the LIMIT/OFFSET query, and NewWindow builds the numbered
// navigation bar — first/last page always shown, a span of pages around the
// current one, ellipses for the gaps, and per-link query strings preserving
// the request's other parameters.
//
// The package runs no queries and imports no driver: it emits (sql, args) the
// caller passes to pgx, database/sql, or any builder. Columns are written
// verbatim and validated as identifiers; cursor values only ever bind as
// arguments.
//
// # Keyset usage
//
//	ks := pagination.Keyset{
//		{Column: "created_at", Desc: true},
//		{Column: "id", Desc: true}, // unique tiebreaker
//	}
//	codec, _ := pagination.NewCodec() // add pagination.WithSigner(s) to sign
//
//	cur, err := codec.Decode(r.URL.Query().Get("cursor")) // zero on first page
//	if err != nil { /* 400 */ }
//	where, _ := ks.Where(cur, pagination.Dollar, 1)
//	order, _ := ks.OrderBy(cur.Backward)
//
//	const size = 20
//	sql := "SELECT id, created_at FROM events"
//	args := []any{}
//	if where.SQL != "" {
//		sql += " WHERE " + where.SQL
//		args = append(args, where.Args...)
//	}
//	sql += " ORDER BY " + order + " LIMIT " + strconv.Itoa(size+1) // +1 sentinel
//
//	rows, _ := scanEvents(db.Query(ctx, sql, args...))
//	page, _ := pagination.NewPage(rows, cur, size, func(e Event) []any {
//		return []any{e.CreatedAt, e.ID}
//	}, codec)
//	// page.Items, page.Next, page.Prev
//
// # Offset usage
//
//	nav := pagination.NewWindow(pagination.WindowConfig{
//		Page: p, PerPage: 25, Total: total, Span: 2, Query: r.URL.Query(),
//	})
//	limit, offset := nav.PerPage, pagination.Offset(nav.Page, nav.PerPage)
package pagination
