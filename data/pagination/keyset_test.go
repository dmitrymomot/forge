package pagination_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/dmitrymomot/forge/data/pagination"
)

func TestKeysetWhere(t *testing.T) {
	t.Parallel()

	single := pagination.Keyset{{Column: "id", Desc: false}}
	multi := pagination.Keyset{
		{Column: "created_at", Desc: true},
		{Column: "id", Desc: true},
	}

	tests := []struct {
		name    string
		ks      pagination.Keyset
		cur     pagination.Cursor
		dialect pagination.Dialect
		start   int
		wantSQL string
		wantArg []any
	}{
		{
			name:    "single asc dollar forward",
			ks:      single,
			cur:     pagination.Cursor{Keys: []any{int64(42)}},
			dialect: pagination.Dollar,
			start:   1,
			wantSQL: "(id > $1)",
			wantArg: []any{int64(42)},
		},
		{
			name:    "single asc question forward",
			ks:      single,
			cur:     pagination.Cursor{Keys: []any{int64(42)}},
			dialect: pagination.Question,
			start:   1,
			wantSQL: "(id > ?)",
			wantArg: []any{int64(42)},
		},
		{
			name:    "multi desc dollar forward",
			ks:      multi,
			cur:     pagination.Cursor{Keys: []any{"2026-01-01", int64(7)}},
			dialect: pagination.Dollar,
			start:   1,
			wantSQL: "((created_at < $1) OR (created_at = $1 AND id < $2))",
			wantArg: []any{"2026-01-01", int64(7)},
		},
		{
			name:    "multi desc question forward repeats prefix",
			ks:      multi,
			cur:     pagination.Cursor{Keys: []any{"2026-01-01", int64(7)}},
			dialect: pagination.Question,
			start:   1,
			wantSQL: "((created_at < ?) OR (created_at = ? AND id < ?))",
			wantArg: []any{"2026-01-01", "2026-01-01", int64(7)},
		},
		{
			name:    "multi desc dollar backward flips ops",
			ks:      multi,
			cur:     pagination.Cursor{Keys: []any{"2026-01-01", int64(7)}, Backward: true},
			dialect: pagination.Dollar,
			start:   1,
			wantSQL: "((created_at > $1) OR (created_at = $1 AND id > $2))",
			wantArg: []any{"2026-01-01", int64(7)},
		},
		{
			name: "mixed directions dollar forward",
			ks: pagination.Keyset{
				{Column: "score", Desc: true},
				{Column: "name", Desc: false},
				{Column: "id", Desc: false},
			},
			cur:     pagination.Cursor{Keys: []any{int64(9), "bob", int64(3)}},
			dialect: pagination.Dollar,
			start:   1,
			wantSQL: "((score < $1) OR (score = $1 AND name > $2) OR (score = $1 AND name = $2 AND id > $3))",
			wantArg: []any{int64(9), "bob", int64(3)},
		},
		{
			name:    "dollar start offset composes after existing args",
			ks:      multi,
			cur:     pagination.Cursor{Keys: []any{"2026-01-01", int64(7)}},
			dialect: pagination.Dollar,
			start:   3,
			wantSQL: "((created_at < $3) OR (created_at = $3 AND id < $4))",
			wantArg: []any{"2026-01-01", int64(7)},
		},
		{
			name:    "zero cursor yields empty fragment",
			ks:      multi,
			cur:     pagination.Cursor{},
			dialect: pagination.Dollar,
			start:   1,
			wantSQL: "",
			wantArg: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := tt.ks.Where(tt.cur, tt.dialect, tt.start)
			if err != nil {
				t.Fatalf("Where: unexpected error: %v", err)
			}
			if got.SQL != tt.wantSQL {
				t.Errorf("SQL:\n got %q\nwant %q", got.SQL, tt.wantSQL)
			}
			if !reflect.DeepEqual(got.Args, tt.wantArg) {
				t.Errorf("Args: got %#v want %#v", got.Args, tt.wantArg)
			}
		})
	}
}

func TestKeysetWhereErrors(t *testing.T) {
	t.Parallel()

	valid := pagination.Keyset{{Column: "id"}}
	tests := []struct {
		name    string
		ks      pagination.Keyset
		cur     pagination.Cursor
		dialect pagination.Dialect
		start   int
		wantErr error
	}{
		{"empty keyset", pagination.Keyset{}, pagination.Cursor{Keys: []any{1}}, pagination.Dollar, 1, pagination.ErrEmptyKeyset},
		{"invalid column", pagination.Keyset{{Column: "id; DROP TABLE"}}, pagination.Cursor{Keys: []any{1}}, pagination.Dollar, 1, pagination.ErrInvalidColumn},
		{"arity mismatch", pagination.Keyset{{Column: "a"}, {Column: "b"}}, pagination.Cursor{Keys: []any{1}}, pagination.Dollar, 1, pagination.ErrCursorArity},
		{"bad dialect", valid, pagination.Cursor{Keys: []any{1}}, pagination.Dialect(9), 1, pagination.ErrInvalidDialect},
		{"dollar start below 1", valid, pagination.Cursor{Keys: []any{1}}, pagination.Dollar, 0, pagination.ErrInvalidStart},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := tt.ks.Where(tt.cur, tt.dialect, tt.start)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("got %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestKeysetWhereQuestionIgnoresStart(t *testing.T) {
	t.Parallel()
	ks := pagination.Keyset{{Column: "id"}}
	got, err := ks.Where(pagination.Cursor{Keys: []any{int64(1)}}, pagination.Question, 0)
	if err != nil {
		t.Fatalf("Question with start 0 should be valid: %v", err)
	}
	if got.SQL != "(id > ?)" {
		t.Errorf("got %q", got.SQL)
	}
}

func TestKeysetOrderBy(t *testing.T) {
	t.Parallel()
	ks := pagination.Keyset{
		{Column: "created_at", Desc: true},
		{Column: "id", Desc: false},
	}
	tests := []struct {
		name     string
		backward bool
		want     string
	}{
		{"forward", false, "created_at DESC, id ASC"},
		{"backward reverses each", true, "created_at ASC, id DESC"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ks.OrderBy(tt.backward)
			if err != nil {
				t.Fatalf("OrderBy: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q want %q", got, tt.want)
			}
		})
	}
}

func TestKeysetOrderByErrors(t *testing.T) {
	t.Parallel()
	if _, err := (pagination.Keyset{}).OrderBy(false); !errors.Is(err, pagination.ErrEmptyKeyset) {
		t.Errorf("empty: got %v", err)
	}
	if _, err := (pagination.Keyset{{Column: "1bad"}}).OrderBy(false); !errors.Is(err, pagination.ErrInvalidColumn) {
		t.Errorf("bad column: got %v", err)
	}
}

func TestValidColumn(t *testing.T) {
	t.Parallel()
	// Exercised through Where, which rejects invalid identifiers.
	bad := []string{"", "a.", ".a", "a..b", "a b", "a-b", "1a", "a.b.c", "a;b", "col)"}
	for _, col := range bad {
		if _, err := (pagination.Keyset{{Column: col}}).OrderBy(false); !errors.Is(err, pagination.ErrInvalidColumn) {
			t.Errorf("column %q: expected ErrInvalidColumn, got %v", col, err)
		}
	}
	good := []string{"id", "created_at", "events.created_at", "_x", "a1"}
	for _, col := range good {
		if _, err := (pagination.Keyset{{Column: col}}).OrderBy(false); err != nil {
			t.Errorf("column %q: unexpected error %v", col, err)
		}
	}
}
