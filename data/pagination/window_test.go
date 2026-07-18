package pagination_test

import (
	"net/url"
	"testing"

	"github.com/dmitrymomot/forge/data/pagination"
)

// bar renders a Window's items as a compact string: page numbers, "." for the
// current page's number wrapper, and "…" for an ellipsis.
func bar(w pagination.Window) string {
	out := ""
	for i, it := range w.Items {
		if i > 0 {
			out += " "
		}
		switch {
		case it.Ellipsis:
			out += "…"
		case it.Current:
			out += "[" + itoa(it.Page) + "]"
		default:
			out += itoa(it.Page)
		}
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

func TestNewWindowBar(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		page, perPage int
		total, span   int
		want          string
	}{
		{"single page", 1, 10, 5, 2, "[1]"},
		{"two pages", 1, 10, 20, 2, "[1] 2"},
		{"no gaps small", 3, 10, 50, 2, "1 2 [3] 4 5"},
		{"gap on the right", 2, 10, 200, 2, "1 [2] 3 4 … 20"},
		{"gaps both sides", 10, 10, 200, 2, "1 … 8 9 [10] 11 12 … 20"},
		{"gap on the left", 19, 10, 200, 2, "1 … 17 18 [19] 20"},
		{"span zero", 6, 10, 200, 0, "1 … [6] … 20"},
		{"adjacent to edges no ellipsis", 3, 10, 200, 2, "1 2 [3] 4 5 … 20"},
		{"single left gap shows digit not ellipsis", 5, 10, 200, 2, "1 2 3 4 [5] 6 7 … 20"},
		{"single right gap shows digit not ellipsis", 16, 10, 200, 2, "1 … 14 15 [16] 17 18 19 20"},
		{"single gaps both sides", 5, 10, 90, 2, "1 2 3 4 [5] 6 7 8 9"},
		{"clamp page above total", 99, 10, 30, 2, "1 2 [3]"},
		{"clamp page below one", -5, 10, 30, 2, "[1] 2 3"},
		{"zero total is one page", 1, 10, 0, 2, "[1]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			w := pagination.NewWindow(pagination.WindowConfig{
				Page: tt.page, PerPage: tt.perPage, Total: tt.total, Span: tt.span,
			})
			if got := bar(w); got != tt.want {
				t.Errorf("bar:\n got %q\nwant %q", got, tt.want)
			}
		})
	}
}

func TestNewWindowMeta(t *testing.T) {
	t.Parallel()
	w := pagination.NewWindow(pagination.WindowConfig{Page: 3, PerPage: 10, Total: 95, Span: 2})
	if w.TotalPages != 10 {
		t.Errorf("TotalPages: got %d want 10", w.TotalPages)
	}
	if w.Page != 3 || w.PerPage != 10 || w.Total != 95 {
		t.Errorf("meta: got page=%d perPage=%d total=%d", w.Page, w.PerPage, w.Total)
	}
	if !w.HasPrev || w.PrevPage != 2 {
		t.Errorf("prev: has=%v page=%d", w.HasPrev, w.PrevPage)
	}
	if !w.HasNext || w.NextPage != 4 {
		t.Errorf("next: has=%v page=%d", w.HasNext, w.NextPage)
	}
}

func TestNewWindowEdgesNoAdjacent(t *testing.T) {
	t.Parallel()
	first := pagination.NewWindow(pagination.WindowConfig{Page: 1, PerPage: 10, Total: 30})
	if first.HasPrev || first.PrevPage != 0 {
		t.Errorf("first page must have no prev: has=%v page=%d", first.HasPrev, first.PrevPage)
	}
	last := pagination.NewWindow(pagination.WindowConfig{Page: 3, PerPage: 10, Total: 30})
	if last.HasNext || last.NextPage != 0 {
		t.Errorf("last page must have no next: has=%v page=%d", last.HasNext, last.NextPage)
	}
}

func TestNewWindowLinksPreserveParams(t *testing.T) {
	t.Parallel()
	q := url.Values{"sort": {"name"}, "status": {"active"}, "page": {"2"}}
	w := pagination.NewWindow(pagination.WindowConfig{
		Page: 2, PerPage: 10, Total: 100, Span: 1, Query: q,
	})
	// Each link carries the other params and sets page to its own number.
	for _, it := range w.Items {
		if it.Ellipsis {
			if it.Query != "" {
				t.Errorf("ellipsis must have empty query, got %q", it.Query)
			}
			continue
		}
		got, err := url.ParseQuery(it.Query)
		if err != nil {
			t.Fatalf("ParseQuery(%q): %v", it.Query, err)
		}
		if got.Get("sort") != "name" || got.Get("status") != "active" {
			t.Errorf("page %d dropped params: %q", it.Page, it.Query)
		}
		if got.Get("page") != itoa(it.Page) {
			t.Errorf("page %d: page param = %q", it.Page, got.Get("page"))
		}
	}
	if w.PrevQuery == "" || w.NextQuery == "" {
		t.Errorf("prev/next queries should be set: prev=%q next=%q", w.PrevQuery, w.NextQuery)
	}
	// The caller's original Query map must not be mutated.
	if q.Get("page") != "2" {
		t.Errorf("NewWindow mutated caller's Query: page=%q", q.Get("page"))
	}
}

func TestNewWindowNilQueryEmptyLinks(t *testing.T) {
	t.Parallel()
	w := pagination.NewWindow(pagination.WindowConfig{Page: 2, PerPage: 10, Total: 100, Span: 2})
	for _, it := range w.Items {
		if it.Query != "" {
			t.Errorf("nil Query should leave item.Query empty, got %q", it.Query)
		}
	}
	if w.PrevQuery != "" || w.NextQuery != "" {
		t.Errorf("nil Query should leave prev/next empty")
	}
}

func TestNewWindowCustomParam(t *testing.T) {
	t.Parallel()
	w := pagination.NewWindow(pagination.WindowConfig{
		Page: 1, PerPage: 10, Total: 100, Query: url.Values{}, Param: "p",
	})
	got, _ := url.ParseQuery(w.NextQuery)
	if got.Get("p") != "2" {
		t.Errorf("custom param: NextQuery=%q", w.NextQuery)
	}
}

func TestPageCount(t *testing.T) {
	t.Parallel()
	cases := []struct {
		total, perPage, want int
	}{
		{0, 10, 1},
		{1, 10, 1},
		{10, 10, 1},
		{11, 10, 2},
		{100, 10, 10},
		{5, 0, 5},   // perPage < 1 treated as 1
		{-3, 10, 1}, // negative total → one page
	}
	for _, tt := range cases {
		if got := pagination.PageCount(tt.total, tt.perPage); got != tt.want {
			t.Errorf("PageCount(%d,%d) = %d, want %d", tt.total, tt.perPage, got, tt.want)
		}
	}
}

func TestOffset(t *testing.T) {
	t.Parallel()
	cases := []struct {
		page, perPage, want int
	}{
		{1, 10, 0},
		{2, 10, 10},
		{3, 25, 50},
		{0, 10, 0},  // clamps page to 1
		{-4, 10, 0}, // clamps page to 1
		{2, 0, 1},   // perPage clamps to 1
	}
	for _, tt := range cases {
		if got := pagination.Offset(tt.page, tt.perPage); got != tt.want {
			t.Errorf("Offset(%d,%d) = %d, want %d", tt.page, tt.perPage, got, tt.want)
		}
	}
}
