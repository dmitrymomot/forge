package render

import (
	"fmt"
	"io"
	"net/http"
)

// Stream copies body to the response inline with the given status code. If contentType
// is non-empty it is used (unless the caller has already set one); otherwise net/http
// sniffs. It is pass-through: a copy error mid-stream may leave a partial body, so the
// returned error is for logging. Use it to proxy an io.Reader (e.g. an upstream or S3
// response body) inline.
func Stream(w http.ResponseWriter, status int, contentType string, body io.Reader) error {
	if contentType != "" {
		setContentType(w, contentType)
	}
	w.WriteHeader(status)
	if _, err := io.Copy(w, body); err != nil {
		return fmt.Errorf("render: stream: %w", err)
	}
	return nil
}

// Attachment is Stream plus a Content-Disposition: attachment header with an RFC
// 5987-safe filename. contentType defaults to "application/octet-stream" when empty
// (download intent). Use it for generated downloads (a built CSV/PDF, an export
// stream, or a proxied object you want saved rather than displayed).
func Attachment(w http.ResponseWriter, status int, filename, contentType string, body io.Reader) error {
	if contentType == "" {
		contentType = contentTypeOctet
	}
	setContentType(w, contentType)
	if filename != "" {
		w.Header().Set("Content-Disposition", contentDisposition("attachment", filename))
	}
	w.WriteHeader(status)
	if _, err := io.Copy(w, body); err != nil {
		return fmt.Errorf("render: attachment: %w", err)
	}
	return nil
}
