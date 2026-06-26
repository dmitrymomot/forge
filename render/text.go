package render

import (
	"fmt"
	"io"
	"net/http"
)

// Text writes s with the given status code as "text/plain; charset=utf-8" (unless the
// caller has already set a Content-Type).
func Text(w http.ResponseWriter, status int, s string) error {
	setContentType(w, contentTypeText)
	w.WriteHeader(status)
	if _, err := io.WriteString(w, s); err != nil {
		return fmt.Errorf("render: write text: %w", err)
	}
	return nil
}

// Blob writes b with the given status code. If contentType is non-empty it is used
// (unless the caller has already set a Content-Type); otherwise net/http sniffs the
// body on first write.
func Blob(w http.ResponseWriter, status int, contentType string, b []byte) error {
	if contentType != "" {
		setContentType(w, contentType)
	}
	w.WriteHeader(status)
	if _, err := w.Write(b); err != nil {
		return fmt.Errorf("render: write blob: %w", err)
	}
	return nil
}

// NoContent writes 204 No Content with no body. It cannot fail, so it returns nothing.
func NoContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}
