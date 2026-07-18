package pagination_test

import (
	"fmt"
	"strings"

	"github.com/dmitrymomot/forge/data/pagination"
)

// Building the WHERE and ORDER BY fragments of a keyset page query. The
// fragments emit (sql, args); the caller runs the query.
func ExampleKeyset_Where() {
	ks := pagination.Keyset{
		{Column: "created_at", Desc: true},
		{Column: "id", Desc: true}, // unique tiebreaker
	}
	cur := pagination.Cursor{Keys: []any{"2026-01-01T00:00:00Z", int64(42)}}

	where, _ := ks.Where(cur, pagination.Dollar, 1)
	order, _ := ks.OrderBy(cur.Backward)

	fmt.Println("WHERE " + where.SQL)
	fmt.Println("ORDER BY " + order)
	fmt.Println(where.Args)
	// Output:
	// WHERE ((created_at < $1) OR (created_at = $1 AND id < $2))
	// ORDER BY created_at DESC, id DESC
	// [2026-01-01T00:00:00Z 42]
}

// A cursor round-trips through an opaque string; large integer keys keep full
// precision.
func ExampleCodec() {
	codec, err := pagination.NewCodec() // add pagination.WithSigner(s) to sign
	if err != nil {
		panic(err)
	}

	enc, _ := codec.Encode(pagination.Cursor{Keys: []any{int64(9007199254740993)}})
	got, _ := codec.Decode(enc)

	fmt.Printf("%d\n", got.Keys[0])
	// Output:
	// 9007199254740993
}

// A numbered navigation bar with ellipses for a deep offset page.
func ExampleNewWindow() {
	w := pagination.NewWindow(pagination.WindowConfig{
		Page: 10, PerPage: 10, Total: 200, Span: 2,
	})

	labels := make([]string, len(w.Items))
	for i, it := range w.Items {
		switch {
		case it.Ellipsis:
			labels[i] = "…"
		case it.Current:
			labels[i] = fmt.Sprintf("[%d]", it.Page)
		default:
			labels[i] = fmt.Sprintf("%d", it.Page)
		}
	}
	fmt.Println(strings.Join(labels, " "))
	// Output:
	// 1 … 8 9 [10] 11 12 … 20
}
