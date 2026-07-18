package pagination

// Option configures Parse and ParseCursor.
type Option func(*config)

// WithPageParams sets the query parameter names for the 1-based page number and page limit. Empty names leave the corresponding default unchanged.
func WithPageParams(page, perPage string) Option {
	return func(c *config) {
		if page != "" {
			c.pageParam = page
		}
		if perPage != "" {
			c.perPageParam = perPage
		}
	}
}

// WithCursorParams sets the query parameter names for the opaque cursor and its limit. Empty names leave the corresponding default unchanged.
func WithCursorParams(cursor, limit string) Option {
	return func(c *config) {
		if cursor != "" {
			c.cursorParam = cursor
		}
		if limit != "" {
			c.cursorLimitParam = limit
		}
	}
}

// WithDefaultLimit sets the limit used when the limit parameter is absent or empty. Non-positive values are ignored. If it exceeds the resolved maximum, both parsers use that maximum instead.
func WithDefaultLimit(limit int32) Option {
	return func(c *config) {
		if limit > 0 {
			c.limit = limit
		}
	}
}

// WithMaxLimit sets the inclusive upper bound for a requested limit. Non-positive values are ignored.
func WithMaxLimit(limit int32) Option {
	return func(c *config) {
		if limit > 0 {
			c.max = limit
		}
	}
}
