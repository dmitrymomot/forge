package opensearch

import (
	"errors"
	"net/http"

	osgo "github.com/opensearch-project/opensearch-go/v4"
)

// IsNotFound reports whether err is (or wraps) an OpenSearch 404 — an absent index
// or document. opensearch-go v4 parses api error responses into *opensearch.StructError
// or *opensearch.StringError, both of which carry an HTTP Status; IsNotFound matches
// either when that status is 404. It returns false for nil and for non-404 errors.
func IsNotFound(err error) bool {
	if err == nil {
		return false
	}
	var structErr *osgo.StructError
	if errors.As(err, &structErr) && structErr.Status == http.StatusNotFound {
		return true
	}
	var stringErr *osgo.StringError
	if errors.As(err, &stringErr) && stringErr.Status == http.StatusNotFound {
		return true
	}
	return false
}
