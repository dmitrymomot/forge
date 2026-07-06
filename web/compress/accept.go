package compress

import (
	"strconv"
	"strings"
)

// negotiate picks gzip or deflate from an Accept-Encoding header, preferring
// gzip on equal q-values. It returns "" when neither is acceptable.
//
// A wildcard "*" sets the acceptability of any content-coding the client did
// not name explicitly (RFC 9110 §12.5.3): "*" alone makes gzip and deflate
// acceptable, while "*;q=0" rejects any coding not otherwise listed. An
// explicit gzip/deflate entry always overrides the wildcard.
func negotiate(header string) string {
	if header == "" {
		return ""
	}
	qGzip, qDeflate, qStar := -1.0, -1.0, -1.0
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
		case "*":
			qStar = q
		}
	}
	// Fall back to the wildcard q-value for any coding not named explicitly.
	if qGzip < 0 {
		qGzip = qStar
	}
	if qDeflate < 0 {
		qDeflate = qStar
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
