package render

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// JSON encodes v as JSON and writes it with the given status code. It is
// transactional: v is encoded into a pooled buffer first, so on an encode error
// nothing is written to w and the error is returned — the caller can still send a
// clean error response. The Content-Type defaults to "application/json; charset=utf-8"
// unless the caller has already set one.
func JSON(w http.ResponseWriter, status int, v any) error {
	buf := getBuf()
	defer putBuf(buf)
	if err := json.NewEncoder(buf).Encode(v); err != nil {
		return fmt.Errorf("render: encode json: %w", err)
	}
	setContentType(w, contentTypeJSON)
	w.WriteHeader(status)
	if _, err := w.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("render: write json: %w", err)
	}
	return nil
}

// JSONStream is the streaming counterpart to JSON: it writes the status, then encodes
// v straight to w with no intermediate buffer. Use it for very large payloads where
// buffering the whole document is wasteful. Unlike JSON it is NOT transactional — an
// encode error mid-stream leaves a partial body under the already-sent status, so the
// returned error is only useful for logging. The Content-Type defaults to
// "application/json; charset=utf-8" unless already set.
func JSONStream(w http.ResponseWriter, status int, v any) error {
	setContentType(w, contentTypeJSON)
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		return fmt.Errorf("render: stream json: %w", err)
	}
	return nil
}
