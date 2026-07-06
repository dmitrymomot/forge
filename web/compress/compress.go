package compress

import (
	"net/http"
	"sync"

	"github.com/dmitrymomot/forge/web/middleware"
)

// defaultTypes are the content types worth compressing. Binary formats
// (images, video, archives) are already compressed and excluded.
var defaultTypes = []string{"text/*", "application/json", "application/javascript", "image/svg+xml"}

// New returns response-compression middleware negotiating gzip/deflate from
// Accept-Encoding. Responses under MinSize, non-matching content types,
// Range requests, HEAD requests, upgrades, and pre-encoded responses pass
// through unchanged. Flusher is preserved: each Flush drains the compressor
// so SSE frames reach the client immediately.
func New(opts ...Option) (middleware.Middleware, error) {
	cf := config{cfg: DefaultConfig(), types: defaultTypes}
	for _, o := range opts {
		o(&cf)
	}
	if err := cf.cfg.Validate(); err != nil {
		return nil, err
	}
	pools := map[string]*sync.Pool{
		"gzip":    newPool("gzip", cf.cfg.Level),
		"deflate": newPool("deflate", cf.cfg.Level),
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Add("Vary", "Accept-Encoding")
			enc := negotiate(r.Header.Get("Accept-Encoding"))
			if enc == "" || r.Method == http.MethodHead ||
				r.Header.Get("Range") != "" || r.Header.Get("Upgrade") != "" {
				next.ServeHTTP(w, r)
				return
			}
			cw := &writer{
				rw:      w,
				rc:      http.NewResponseController(w),
				pool:    pools[enc],
				enc:     enc,
				types:   cf.types,
				minSize: cf.cfg.MinSize,
			}
			defer cw.close()
			next.ServeHTTP(cw, r)
		})
	}, nil
}
