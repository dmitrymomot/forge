package problem

import (
	"net/http"

	"github.com/dmitrymomot/forge/render"
)

// JSON returns a Responder that writes err as application/problem+json (RFC 9457).
// When configured WithLogger, 5xx errors are logged (never placed in the body).
func JSON(opts ...Option) Responder {
	c := newConfig(opts...)
	return func(w http.ResponseWriter, r *http.Request, err error) {
		p := c.build(err, r)
		c.log5xx(r, p, err)
		// Set the content type first; render.JSON preserves a preset content type.
		w.Header().Set("Content-Type", "application/problem+json")
		_ = render.JSON(w, p.Status, p)
	}
}
