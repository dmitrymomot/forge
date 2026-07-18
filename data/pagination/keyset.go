package pagination

import (
	"strconv"
	"strings"
)

// Dialect selects the placeholder style of an emitted SQL fragment.
type Dialect uint8

const (
	// Dollar numbers placeholders $1, $2, … (PostgreSQL / pgx). It is the
	// zero value and the default.
	Dollar Dialect = iota
	// Question uses positional ? placeholders (ClickHouse, MySQL, SQLite).
	Question
)

// Order names one column of a keyset ordering and its direction.
type Order struct {
	// Column is the ordered column: a plain, optionally table-qualified SQL
	// identifier (e.g. "created_at" or "events.created_at"). It is written
	// into SQL verbatim, so it must be a trusted identifier, never user input.
	Column string
	// Desc orders the column descending when true, ascending when false.
	Desc bool
}

// Keyset is the ordered list of columns a keyset (cursor) query sorts and
// pages by. It must form a strict total order: the final column has to be
// unique (a primary key or ULID) so no two rows compare equal and a page
// never splits or repeats a row. Every column must be NOT NULL — a NULL
// keyset value makes the comparison NULL and silently drops rows.
type Keyset []Order

// Fragment is a parameterized SQL fragment: SQL text plus the arguments its
// placeholders bind, in placeholder order. The caller concatenates SQL into
// the query and appends Args to the query's argument list.
type Fragment struct {
	// SQL is the fragment text, already parenthesized so it can be safely
	// joined with AND/OR. It is empty for the first page (a zero cursor).
	SQL string
	// Args are the values the fragment's placeholders bind, in the order the
	// placeholders appear (Question) or by ascending number (Dollar).
	Args []any
}

// Where builds the keyset comparison selecting the rows strictly after cur
// (before, for a backward cursor) in the keyset's order — the WHERE fragment
// of a keyset page query. A zero cursor yields an empty Fragment (the first
// page needs no comparison).
//
// The comparison is emitted as the portable lexicographic OR-of-ANDs
// expansion (row-value comparisons are not honored uniformly across engines),
// e.g. for ("created_at" DESC, "id" DESC):
//
//	((created_at < $1) OR (created_at = $1 AND id < $2))
//
// start is the number of the fragment's first placeholder (1-based) for the
// Dollar dialect, so the fragment composes after a query's existing
// arguments; it is ignored by Question. Dollar reuses each column's
// placeholder, so Args holds one value per column; Question cannot reuse
// positional placeholders, so Args repeats the prefix values.
func (k Keyset) Where(cur Cursor, d Dialect, start int) (Fragment, error) {
	if err := k.validate(); err != nil {
		return Fragment{}, err
	}
	if d != Dollar && d != Question {
		return Fragment{}, ErrInvalidDialect
	}
	if d == Dollar && start < 1 {
		return Fragment{}, ErrInvalidStart
	}
	if cur.IsZero() {
		return Fragment{}, nil
	}
	if len(cur.Keys) != len(k) {
		return Fragment{}, ErrCursorArity
	}

	n := len(k)
	var b strings.Builder
	// Dollar binds each column once and reuses the placeholder; Question
	// repeats prefix values, so it needs 1+2+…+n = n(n+1)/2 slots.
	args := make([]any, 0, n*(n+1)/2)
	if d == Dollar {
		args = append(args, cur.Keys...)
	}

	writePlaceholder := func(col int) {
		if d == Dollar {
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(start + col))
			return
		}
		b.WriteByte('?')
		args = append(args, cur.Keys[col])
	}

	b.WriteByte('(')
	for i := range n {
		if i > 0 {
			b.WriteString(" OR ")
		}
		if n > 1 {
			b.WriteByte('(')
		}
		for j := range i { // equality prefix on the columns before i
			b.WriteString(k[j].Column)
			b.WriteString(" = ")
			writePlaceholder(j)
			b.WriteString(" AND ")
		}
		b.WriteString(k[i].Column)
		b.WriteByte(' ')
		b.WriteString(strictOp(k[i].Desc, cur.Backward))
		b.WriteByte(' ')
		writePlaceholder(i)
		if n > 1 {
			b.WriteByte(')')
		}
	}
	b.WriteByte(')')

	return Fragment{SQL: b.String(), Args: args}, nil
}

// OrderBy builds the keyset's ORDER BY column list (without the "ORDER BY "
// keyword), e.g. "created_at DESC, id DESC". A backward cursor reverses every
// column's direction so the previous page can be fetched in reverse and its
// rows restored to display order (NewPage does the restoring).
func (k Keyset) OrderBy(backward bool) (string, error) {
	if err := k.validate(); err != nil {
		return "", err
	}
	var b strings.Builder
	for i, o := range k {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(o.Column)
		if o.Desc != backward { // Desc XOR backward
			b.WriteString(" DESC")
		} else {
			b.WriteString(" ASC")
		}
	}
	return b.String(), nil
}

func (k Keyset) validate() error {
	if len(k) == 0 {
		return ErrEmptyKeyset
	}
	for _, o := range k {
		if !validColumn(o.Column) {
			return ErrInvalidColumn
		}
	}
	return nil
}

// strictOp returns the strict comparison operator selecting rows after the
// cursor for a column: ascending forward is ">", descending forward is "<",
// and a backward cursor flips both.
func strictOp(desc, backward bool) string {
	if desc != backward { // Desc XOR backward
		return "<"
	}
	return ">"
}

// validColumn reports whether s is an SQL identifier of the form part or
// part.part, where each part is [A-Za-z_][A-Za-z0-9_]*.
func validColumn(s string) bool {
	dots := 0
	atPartStart := true
	for i := range len(s) {
		switch c := s[i]; {
		case c == '.':
			if atPartStart {
				return false
			}
			if dots++; dots > 1 {
				return false
			}
			atPartStart = true
		case c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z'):
			atPartStart = false
		case c >= '0' && c <= '9':
			if atPartStart {
				return false
			}
		default:
			return false
		}
	}
	return !atPartStart // rejects "", trailing dot, and empty parts
}
