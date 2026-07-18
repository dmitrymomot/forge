package pagination

// Page is one page of keyset results plus the opaque cursors to fetch the
// adjacent pages. A cursor is empty when there is no such page: Prev is empty
// on the first page, Next on the last.
type Page[T any] struct {
	// Next fetches the following page (a forward cursor); empty on the last
	// page.
	Next string
	// Prev fetches the preceding page (a backward cursor); empty on the first
	// page.
	Prev string
	// Items are the rows of this page, in display order.
	Items []T
}

// KeyFunc extracts a row's keyset values in the keyset's column order — the
// same columns, in the same order, as the Keyset driving the query.
type KeyFunc[T any] func(T) []any

// NewPage assembles a Page from the rows a keyset query returned. Fetch one
// more row than the page size (LIMIT size+1) so NewPage can detect an adjacent
// page; it trims that sentinel. cur is the request's decoded cursor: its
// Backward flag and whether it is zero decide which adjacent cursors exist.
//
// Pass rows exactly as the query returned them. For a backward request the
// query's ORDER BY is reversed (Keyset.OrderBy with backward true), so rows
// arrive in reverse display order; NewPage restores display order. key must
// extract the same columns the keyset orders by.
//
// size must be at least 1 (ErrInvalidSize otherwise), so a size taken from an
// unvalidated request fails closed rather than panicking on a slice bound.
func NewPage[T any](rows []T, cur Cursor, size int, key KeyFunc[T], codec *Codec) (Page[T], error) {
	if size < 1 {
		return Page[T]{}, ErrInvalidSize
	}
	hasExtra := len(rows) > size
	if hasExtra {
		rows = rows[:size] // drop the sentinel (last row in the query's order)
	}
	if cur.Backward {
		reverse(rows) // reverse-ordered fetch → display order
	}

	page := Page[T]{Items: rows}
	if len(rows) == 0 {
		return page, nil
	}

	first := Cursor{Keys: key(rows[0]), Backward: true}
	last := Cursor{Keys: key(rows[len(rows)-1]), Backward: false}

	if cur.Backward {
		// Came from a later page, so a next page always exists; an earlier
		// page exists only when a sentinel row was trimmed.
		next, err := codec.Encode(last)
		if err != nil {
			return Page[T]{}, err
		}
		page.Next = next
		if hasExtra {
			prev, err := codec.Encode(first)
			if err != nil {
				return Page[T]{}, err
			}
			page.Prev = prev
		}
		return page, nil
	}

	// Forward (or first page): a next page exists only when a sentinel row
	// was trimmed; a previous page exists only when the request had a cursor.
	if hasExtra {
		next, err := codec.Encode(last)
		if err != nil {
			return Page[T]{}, err
		}
		page.Next = next
	}
	if !cur.IsZero() {
		prev, err := codec.Encode(first)
		if err != nil {
			return Page[T]{}, err
		}
		page.Prev = prev
	}
	return page, nil
}

func reverse[T any](s []T) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}
