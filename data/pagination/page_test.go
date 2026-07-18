package pagination_test

import (
	"testing"

	"github.com/dmitrymomot/forge/data/pagination"
)

type row struct{ id int64 }

func rowKey(r row) []any { return []any{r.id} }

// rows builds a slice of rows with the given ids.
func rows(ids ...int64) []row {
	out := make([]row, len(ids))
	for i, id := range ids {
		out[i] = row{id: id}
	}
	return out
}

func ids(rs []row) []int64 {
	out := make([]int64, len(rs))
	for i, r := range rs {
		out[i] = r.id
	}
	return out
}

// decodeIDs decodes a cursor and returns its single int64 key plus its
// Backward flag; the cursor must be non-empty.
func decodeIDs(t *testing.T, codec *pagination.Codec, cur string) (int64, bool) {
	t.Helper()
	if cur == "" {
		t.Fatal("expected a non-empty cursor")
	}
	c, err := codec.Decode(cur)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	return c.Keys[0].(int64), c.Backward
}

func TestNewPageForwardFirstPageHasMore(t *testing.T) {
	t.Parallel()
	codec := newCodec(t)
	const size = 3
	// Fetched size+1 rows: a next page exists, no cursor was sent → no prev.
	fetched := rows(10, 9, 8, 7)
	page, err := pagination.NewPage(fetched, pagination.Cursor{}, size, rowKey, codec)
	if err != nil {
		t.Fatalf("NewPage: %v", err)
	}
	if got := ids(page.Items); !equalIDs(got, []int64{10, 9, 8}) {
		t.Errorf("Items: got %v", got)
	}
	if page.Prev != "" {
		t.Errorf("first page must have no Prev, got %q", page.Prev)
	}
	if page.Next == "" {
		t.Fatal("expected a Next cursor")
	}
	if key, backward := decodeIDs(t, codec, page.Next); key != 8 || backward {
		t.Errorf("Next: got key=%d backward=%v, want 8/false", key, backward)
	}
}

func TestNewPageForwardFirstPageNoMore(t *testing.T) {
	t.Parallel()
	codec := newCodec(t)
	const size = 3
	page, err := pagination.NewPage(rows(10, 9), pagination.Cursor{}, size, rowKey, codec)
	if err != nil {
		t.Fatalf("NewPage: %v", err)
	}
	if page.Next != "" || page.Prev != "" {
		t.Errorf("single short page: Next=%q Prev=%q, want both empty", page.Next, page.Prev)
	}
	if got := ids(page.Items); !equalIDs(got, []int64{10, 9}) {
		t.Errorf("Items: got %v", got)
	}
}

func TestNewPageForwardMiddlePage(t *testing.T) {
	t.Parallel()
	codec := newCodec(t)
	const size = 2
	// A forward request that arrived with a cursor → prev exists; extra row → next exists.
	req := pagination.Cursor{Keys: []any{int64(11)}, Backward: false}
	page, err := pagination.NewPage(rows(9, 8, 7), req, size, rowKey, codec)
	if err != nil {
		t.Fatalf("NewPage: %v", err)
	}
	if got := ids(page.Items); !equalIDs(got, []int64{9, 8}) {
		t.Errorf("Items: got %v", got)
	}
	if key, backward := decodeIDs(t, codec, page.Next); key != 8 || backward {
		t.Errorf("Next: got %d/%v want 8/false", key, backward)
	}
	if key, backward := decodeIDs(t, codec, page.Prev); key != 9 || !backward {
		t.Errorf("Prev: got %d/%v want 9/true", key, backward)
	}
}

func TestNewPageBackward(t *testing.T) {
	t.Parallel()
	codec := newCodec(t)
	const size = 2
	// Backward query returns rows in reversed (ascending) order with a sentinel.
	// Display order is DESC, so the previous page of {..,8,9} before a later
	// page is {9,8}; fetched reversed = {8,9,10-sentinel}.
	req := pagination.Cursor{Keys: []any{int64(7)}, Backward: true}
	fetched := rows(8, 9, 10) // reversed order, 10 is the sentinel (earliest-back)
	page, err := pagination.NewPage(fetched, req, size, rowKey, codec)
	if err != nil {
		t.Fatalf("NewPage: %v", err)
	}
	// Sentinel (10) trimmed, remaining {8,9} reversed to display order {9,8}.
	if got := ids(page.Items); !equalIDs(got, []int64{9, 8}) {
		t.Errorf("Items: got %v want [9 8]", got)
	}
	// Next always exists coming from a later page; from last display row (8), forward.
	if key, backward := decodeIDs(t, codec, page.Next); key != 8 || backward {
		t.Errorf("Next: got %d/%v want 8/false", key, backward)
	}
	// Sentinel present → an earlier page exists; Prev from first display row (9), backward.
	if key, backward := decodeIDs(t, codec, page.Prev); key != 9 || !backward {
		t.Errorf("Prev: got %d/%v want 9/true", key, backward)
	}
}

func TestNewPageBackwardReachesFirstPage(t *testing.T) {
	t.Parallel()
	codec := newCodec(t)
	const size = 3
	// Backward with no sentinel → we've reached the first page: no Prev.
	req := pagination.Cursor{Keys: []any{int64(7)}, Backward: true}
	fetched := rows(8, 9, 10) // exactly size, reversed
	page, err := pagination.NewPage(fetched, req, size, rowKey, codec)
	if err != nil {
		t.Fatalf("NewPage: %v", err)
	}
	if got := ids(page.Items); !equalIDs(got, []int64{10, 9, 8}) {
		t.Errorf("Items: got %v want [10 9 8]", got)
	}
	if page.Prev != "" {
		t.Errorf("first page reached: Prev should be empty, got %q", page.Prev)
	}
	if page.Next == "" {
		t.Error("expected a Next cursor")
	}
}

func TestNewPageEmpty(t *testing.T) {
	t.Parallel()
	codec := newCodec(t)
	page, err := pagination.NewPage([]row{}, pagination.Cursor{Keys: []any{int64(1)}}, 3, rowKey, codec)
	if err != nil {
		t.Fatalf("NewPage: %v", err)
	}
	if len(page.Items) != 0 || page.Next != "" || page.Prev != "" {
		t.Errorf("empty result: got items=%d next=%q prev=%q", len(page.Items), page.Next, page.Prev)
	}
}

func TestNewPageEncodeErrorPropagates(t *testing.T) {
	t.Parallel()
	codec := newCodec(t)
	// A key func returning an unmarshalable value makes the cursor Encode
	// fail; NewPage must return that error rather than a half-built page.
	badKey := func(row) []any { return []any{make(chan int)} }

	// Forward with a sentinel row → Next is encoded.
	if _, err := pagination.NewPage(rows(3, 2, 1), pagination.Cursor{}, 2, badKey, codec); err == nil {
		t.Error("forward: expected an encode error")
	}
	// Backward → Next is always encoded.
	req := pagination.Cursor{Keys: []any{int64(9)}, Backward: true}
	if _, err := pagination.NewPage(rows(1, 2, 3), req, 2, badKey, codec); err == nil {
		t.Error("backward: expected an encode error")
	}

	// Forward Prev branch: no sentinel (so Next is skipped) but a request
	// cursor is present, so only the Prev cursor (from the first row) encodes.
	condKey := func(r row) []any {
		if r.id < 0 {
			return []any{make(chan int)}
		}
		return []any{r.id}
	}
	if _, err := pagination.NewPage(rows(-1, 8), pagination.Cursor{Keys: []any{int64(11)}}, 2, condKey, codec); err == nil {
		t.Error("forward prev: expected an encode error")
	}
}

func equalIDs(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
