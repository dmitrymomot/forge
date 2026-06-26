package render

import (
	"encoding/csv"
	"fmt"
	"net/http"
)

// CSV streams records as "text/csv; charset=utf-8" with the given status code. When
// filename is non-empty a Content-Disposition: attachment header is set with an RFC
// 5987-safe filename. CSV streams (it is not buffered), so a write error mid-output
// may leave a partial body; the returned error is for logging.
func CSV(w http.ResponseWriter, status int, filename string, records [][]string) error {
	setContentType(w, contentTypeCSV)
	if filename != "" {
		w.Header().Set("Content-Disposition", contentDisposition("attachment", filename))
	}
	w.WriteHeader(status)
	cw := csv.NewWriter(w)
	if err := cw.WriteAll(records); err != nil { // WriteAll flushes internally
		return fmt.Errorf("render: write csv: %w", err)
	}
	return nil
}
