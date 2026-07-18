package pagination

import (
	"math"
	"net/http"
	"net/url"
	"strconv"

	"github.com/dmitrymomot/forge/web/request"
)

// Params are the bounded LIMIT/OFFSET values for a static list query. They map directly to the common sqlc-generated fields of the same names.
type Params struct {
	Limit  int32
	Offset int32
}

// CursorParams are the bounded opaque-cursor values for a static list query. An empty Cursor identifies the first page; the helper intentionally does not decode or encode it.
type CursorParams struct {
	Cursor string
	Limit  int32
}

// Parse reads a 1-based page number and page limit from r's query and derives SQLC-ready LIMIT/OFFSET parameters. By default it reads "page" and "per_page", uses a limit of 20, and clamps the requested limit to 1..100.
//
// Missing or empty inputs use their defaults. Numeric page values below 1 clamp to 1; numeric limit values below 1 clamp to 1. A non-numeric or int32-overflowing input returns a *request.Error. A page/limit combination whose derived offset does not fit int32 returns a *request.Error wrapping ErrOffsetOverflow.
func Parse(r *http.Request, opts ...Option) (Params, error) {
	if len(opts) == 0 {
		return parse(r, defaultConfig())
	}
	return parse(r, newConfig(opts))
}

func parse(r *http.Request, c config) (Params, error) {
	query := queryValues(r)
	page, err := queryInt32(query, c.pageParam, 1)
	if err != nil {
		return Params{}, err
	}
	limit, err := queryInt32(query, c.perPageParam, c.limit)
	if err != nil {
		return Params{}, err
	}
	page = max(page, 1)
	limit = min(max(limit, 1), c.max)

	offset := int64(page-1) * int64(limit)
	if offset > math.MaxInt32 {
		return Params{}, &request.Error{
			Err:    ErrOffsetOverflow,
			Source: request.SourceQuery,
			Key:    c.pageParam,
			Kind:   request.KindMalformed,
		}
	}
	return Params{Limit: limit, Offset: int32(offset)}, nil
}

// ParseCursor reads an opaque cursor and limit from r's query. By default it reads "cursor" and "limit", uses a limit of 20, and clamps the requested limit to 1..100. Missing or empty cursors identify the first page. A non-numeric or int32-overflowing limit returns a *request.Error.
func ParseCursor(r *http.Request, opts ...Option) (CursorParams, error) {
	if len(opts) == 0 {
		return parseCursor(r, defaultConfig())
	}
	return parseCursor(r, newConfig(opts))
}

func parseCursor(r *http.Request, c config) (CursorParams, error) {
	query := queryValues(r)
	limit, err := queryInt32(query, c.cursorLimitParam, c.limit)
	if err != nil {
		return CursorParams{}, err
	}
	return CursorParams{Cursor: query.Get(c.cursorParam), Limit: min(max(limit, 1), c.max)}, nil
}

func queryValues(r *http.Request) url.Values {
	if r.URL.RawQuery == "" {
		return nil
	}
	return r.URL.Query()
}

func queryInt32(query url.Values, key string, def int32) (int32, error) {
	raw := query.Get(key)
	if raw == "" {
		return def, nil
	}
	n, err := strconv.ParseInt(raw, 10, 32)
	if err != nil {
		return 0, &request.Error{
			Err:    err,
			Source: request.SourceQuery,
			Key:    key,
			Kind:   request.KindMalformed,
		}
	}
	return int32(n), nil
}
