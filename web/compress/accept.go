package compress

import (
	"strconv"
	"strings"
)

// negotiate picks gzip or deflate from an Accept-Encoding header, preferring
// gzip on equal q-values. It returns "" when neither is acceptable.
func negotiate(header string) string {
	if header == "" {
		return ""
	}
	qGzip, qDeflate := -1.0, -1.0
	for part := range strings.SplitSeq(header, ",") {
		name, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		name = strings.ToLower(strings.TrimSpace(name))
		q := 1.0
		if qs, ok := strings.CutPrefix(strings.TrimSpace(params), "q="); ok {
			if v, err := strconv.ParseFloat(strings.TrimSpace(qs), 64); err == nil {
				q = v
			}
		}
		switch name {
		case "gzip", "x-gzip":
			qGzip = q
		case "deflate":
			qDeflate = q
		}
	}
	switch {
	case qGzip > 0 && qGzip >= qDeflate:
		return "gzip"
	case qDeflate > 0:
		return "deflate"
	default:
		return ""
	}
}
