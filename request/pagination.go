package request

import "net/http"

// Page is offset-based pagination input. Offset is derived as (Number-1)*Size.
type Page struct {
	Number int
	Size   int
	Offset int
}

// Cursor is cursor-based pagination input. Value is an opaque token passed through
// verbatim; decoding its payload is the caller's job.
type Cursor struct {
	Value string
	Limit int
}

type pageConfig struct {
	pageKey     string
	sizeKey     string
	cursorKey   string
	limitKey    string
	defaultSize int
	maxSize     int
}

// PageOption configures QueryPage and QueryCursor.
type PageOption func(*pageConfig)

// WithPageParams sets the query parameter names for page number and size.
func WithPageParams(pageKey, sizeKey string) PageOption {
	return func(c *pageConfig) { c.pageKey = pageKey; c.sizeKey = sizeKey }
}

// WithCursorParams sets the query parameter names for cursor and limit.
func WithCursorParams(cursorKey, limitKey string) PageOption {
	return func(c *pageConfig) { c.cursorKey = cursorKey; c.limitKey = limitKey }
}

// WithDefaultPageSize sets the size/limit used when the parameter is absent.
func WithDefaultPageSize(n int) PageOption {
	return func(c *pageConfig) { c.defaultSize = n }
}

// WithMaxPageSize sets the upper bound that size/limit is clamped to.
func WithMaxPageSize(n int) PageOption {
	return func(c *pageConfig) { c.maxSize = n }
}

func newPageConfig(opts []PageOption) pageConfig {
	c := pageConfig{
		pageKey:     "page",
		sizeKey:     "per_page",
		cursorKey:   "cursor",
		limitKey:    "limit",
		defaultSize: 20,
		maxSize:     100,
	}
	for _, o := range opts {
		o(&c)
	}
	return c
}

// QueryPage reads offset-based pagination from the query. A non-numeric value is a
// Malformed error (-> 400); a valid out-of-range value is clamped (page >= 1,
// 1 <= size <= max).
func QueryPage(r *http.Request, opts ...PageOption) (Page, error) {
	c := newPageConfig(opts)

	number, err := Query[int](r, c.pageKey, 1)
	if err != nil {
		return Page{}, err
	}
	size, err := Query[int](r, c.sizeKey, c.defaultSize)
	if err != nil {
		return Page{}, err
	}
	number = max(number, 1)
	size = min(max(size, 1), c.maxSize)
	return Page{Number: number, Size: size, Offset: (number - 1) * size}, nil
}

// QueryCursor reads cursor-based pagination from the query. The cursor is opaque;
// limit shares the same default/max bounds as page size.
func QueryCursor(r *http.Request, opts ...PageOption) (Cursor, error) {
	c := newPageConfig(opts)

	limit, err := Query[int](r, c.limitKey, c.defaultSize)
	if err != nil {
		return Cursor{}, err
	}
	value, err := Query[string](r, c.cursorKey)
	if err != nil {
		return Cursor{}, err
	}
	return Cursor{Value: value, Limit: min(max(limit, 1), c.maxSize)}, nil
}
