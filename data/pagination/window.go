package pagination

import (
	"maps"
	"net/url"
	"strconv"
)

// WindowConfig parameterizes NewWindow.
type WindowConfig struct {
	// Query, when non-nil, is the current request's query values. NewWindow
	// copies it per link and overwrites Param, so each WindowItem and the
	// Prev/Next links carry a ready query string preserving all other params
	// (filters, sort). Nil leaves the Query fields empty.
	Query url.Values
	// Param is the query parameter naming the page (default "page").
	Param string
	// Page is the current page, 1-based. Values outside [1, total pages] clamp.
	Page int
	// PerPage is the page size. Values below 1 are treated as 1.
	PerPage int
	// Total is the total item count across all pages.
	Total int
	// Span is how many page links show on each side of the current page
	// before an ellipsis (a common value is 2). Negative values are treated
	// as 0.
	Span int
}

// WindowItem is one entry in the page-number bar: a page link, the current
// page, or an ellipsis standing in for a skipped range.
type WindowItem struct {
	// Query is the encoded query string for this page's link (e.g.
	// "page=3&sort=name"), set when WindowConfig.Query is non-nil; empty for
	// an ellipsis.
	Query string
	// Page is the page number this item links to; 0 for an ellipsis.
	Page int
	// Current marks the item for the current page.
	Current bool
	// Ellipsis marks a gap standing in for skipped pages.
	Ellipsis bool
}

// Window is the view-model for a numbered pagination bar in server-rendered
// navigation: the elided run of page links plus the resolved previous/next
// links. It is built by NewWindow from an offset-paginated request.
type Window struct {
	// PrevQuery and NextQuery are the encoded query strings for the previous
	// and next pages, set when WindowConfig.Query is non-nil; empty when that
	// direction has no page.
	PrevQuery string
	NextQuery string
	// Items is the page-number bar, in display order, with ellipses inserted.
	Items []WindowItem
	// Page is the effective (clamped) current page.
	Page int
	// PerPage is the effective page size (at least 1).
	PerPage int
	// Total is the total item count.
	Total int
	// TotalPages is the number of pages (at least 1).
	TotalPages int
	// PrevPage and NextPage are the adjacent page numbers; 0 when absent.
	PrevPage int
	NextPage int
	// HasPrev and HasNext report whether an adjacent page exists.
	HasPrev bool
	HasNext bool
}

// NewWindow computes the numbered navigation bar for an offset-paginated
// request. It always shows the first and last page, a run of Span pages on
// each side of the current page, and an ellipsis wherever pages are skipped.
func NewWindow(cfg WindowConfig) Window {
	perPage := max(cfg.PerPage, 1)
	span := max(cfg.Span, 0)
	total := max(cfg.Total, 0)

	totalPages := PageCount(total, perPage)
	page := min(max(cfg.Page, 1), totalPages)

	param := cfg.Param
	if param == "" {
		param = "page"
	}
	// Encode the other query params once, then append "&param=N" per link,
	// instead of cloning and re-encoding the whole map for every page number.
	var prefix string
	if cfg.Query != nil {
		base := maps.Clone(cfg.Query)
		base.Del(param)
		if enc := base.Encode(); enc != "" {
			prefix = enc + "&"
		}
	}
	pair := url.QueryEscape(param) + "="
	link := func(n int) string {
		if cfg.Query == nil {
			return ""
		}
		return prefix + pair + strconv.Itoa(n)
	}

	w := Window{
		Page:       page,
		PerPage:    perPage,
		Total:      total,
		TotalPages: totalPages,
		HasPrev:    page > 1,
		HasNext:    page < totalPages,
	}
	if w.HasPrev {
		w.PrevPage = page - 1
		w.PrevQuery = link(page - 1)
	}
	if w.HasNext {
		w.NextPage = page + 1
		w.NextQuery = link(page + 1)
	}

	item := func(n int) WindowItem {
		return WindowItem{Page: n, Current: n == page, Query: link(n)}
	}
	ellipsis := WindowItem{Ellipsis: true}

	// 1 … [page-span, page+span] … totalPages. An ellipsis stands in only for
	// a gap of two or more pages; a one-page gap shows that page's digit,
	// since the ellipsis would cost the same space yet not be clickable.
	w.Items = append(w.Items, item(1))
	start := max(2, page-span)
	end := min(totalPages-1, page+span)
	switch {
	case start == 3:
		w.Items = append(w.Items, item(2))
	case start > 3:
		w.Items = append(w.Items, ellipsis)
	}
	for n := start; n <= end; n++ {
		w.Items = append(w.Items, item(n))
	}
	switch {
	case end == totalPages-2:
		w.Items = append(w.Items, item(totalPages-1))
	case end < totalPages-2:
		w.Items = append(w.Items, ellipsis)
	}
	if totalPages > 1 {
		w.Items = append(w.Items, item(totalPages))
	}
	return w
}

// PageCount returns the number of pages holding total items at perPage per
// page — at least 1, so an empty result still has a first page. perPage below
// 1 is treated as 1.
func PageCount(total, perPage int) int {
	if perPage < 1 {
		perPage = 1
	}
	if total <= 0 {
		return 1
	}
	return (total + perPage - 1) / perPage
}

// Offset returns the SQL OFFSET for a 1-based page at perPage per page: the
// count of rows to skip. page below 1 and perPage below 1 clamp to 1, so the
// result is never negative.
func Offset(page, perPage int) int {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 1
	}
	return (page - 1) * perPage
}
