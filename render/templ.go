package render

import (
	"context"
	"fmt"
	"io"
	"net/http"
)

// Component is anything that can render itself to an io.Writer. It is structurally
// satisfied by github.com/a-h/templ components (templ.Component has the identical
// Render method), so templ output works without this package importing templ.
type Component interface {
	Render(ctx context.Context, w io.Writer) error
}

// Templ renders c into a pooled buffer, then writes the result with the given status
// code. It is transactional: a Render error returns with nothing written to w. It
// returns ErrNilComponent if c is nil (before writing anything). ctx is the
// per-request context (usually r.Context()). The Content-Type defaults to
// "text/html; charset=utf-8" unless the caller has already set one.
func Templ(ctx context.Context, w http.ResponseWriter, status int, c Component) error {
	if c == nil {
		return ErrNilComponent
	}
	buf := getBuf()
	defer putBuf(buf)
	if err := c.Render(ctx, buf); err != nil {
		return fmt.Errorf("render: render component: %w", err)
	}
	setContentType(w, contentTypeHTML)
	w.WriteHeader(status)
	if _, err := w.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("render: write templ: %w", err)
	}
	return nil
}
