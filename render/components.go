package render

import (
	"context"
	"fmt"
	"net/http"
)

// Components renders each component into one pooled buffer in order, then writes the
// result with the given status. It is transactional with respect to rendering: an empty
// input (ErrNoComponents), a nil element (ErrNilComponent), or any Render error returns
// before a single byte is written to w. A write error after WriteHeader has been called
// is non-recoverable and returned for logging only (the same contract as Templ). The
// Content-Type defaults to "text/html; charset=utf-8" unless the caller has already set one.
//
// Use it for multi-fragment responses — for example an HTMX main fragment plus
// out-of-band fragments whose markup carries hx-swap-oob.
func Components(ctx context.Context, w http.ResponseWriter, status int, components ...Component) error {
	if len(components) == 0 {
		return ErrNoComponents
	}
	buf := getBuf()
	defer putBuf(buf)
	for _, c := range components {
		if c == nil {
			return ErrNilComponent
		}
		if err := c.Render(ctx, buf); err != nil {
			return fmt.Errorf("render: render component: %w", err)
		}
	}
	setContentType(w, contentTypeHTML)
	w.WriteHeader(status)
	if _, err := w.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("render: write components: %w", err)
	}
	return nil
}
