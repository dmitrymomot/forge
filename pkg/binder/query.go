package binder

import (
	"net/http"
)

// Query creates a query parameter binder function.
//
// It supports struct tags for custom parameter names:
//   - `query:"name"` - binds to query parameter "name"
//   - `query:"-"` - skips the field
//   - `query:"name,omitempty"` - same as query:"name" for parsing
//
// Supported types:
//   - Basic types: string, int, int64, uint, uint64, float32, float64, bool
//   - Slices of basic types for multi-value parameters
//   - Pointers for optional fields
//
// Example:
//
//	type SearchRequest struct {
//		Query    string   `query:"q"`
//		Page     int      `query:"page"`
//		PageSize int      `query:"page_size"`
//		Tags     []string `query:"tags"`     // ?tags=go&tags=web or ?tags=go,web
//		Active   *bool    `query:"active"`   // Optional
//		Internal string   `query:"-"`        // Skipped
//	}
//
//	func searchHandler(w http.ResponseWriter, r *http.Request) {
//		var req SearchRequest
//		if err := binder.Query()(r, &req); err != nil {
//			http.Error(w, err.Error(), http.StatusBadRequest)
//			return
//		}
//		// req is populated from query parameters
//		// Process req and return response...
//	}
//
//	http.HandleFunc("/search", searchHandler)
func Query() Binder {
	return func(r *http.Request, v any) error {
		return bindToStruct(v, "query", r.URL.Query(), ErrFailedToParseQuery)
	}
}
