package htmx

import (
	"bytes"
	"net/http"
	"strconv"
)

// Rewriter answers an htmx request whose original response htmx would have dropped.
// It receives the status and the buffered body the wrapped handler produced, and owns
// the real ResponseWriter from that point on.
type Rewriter func(w http.ResponseWriter, r *http.Request, status int, body []byte)

// audibleConfig holds the per-status rewriters.
type audibleConfig struct {
	byStatus map[int]Rewriter
}

// AudibleOption configures NewAudible.
type AudibleOption func(*audibleConfig)

// WithRewriter registers rw for one status. Registering a status twice keeps the last
// rewriter; registering a nil rewriter removes the entry, which is how the built-in
// redirect handling is switched off.
func WithRewriter(status int, rw Rewriter) AudibleOption {
	return func(c *audibleConfig) {
		if rw == nil {
			delete(c.byStatus, status)
			return
		}
		c.byStatus[status] = rw
	}
}

// NewAudible returns middleware that says the answers htmx would otherwise drop.
//
// htmx swaps nothing outside 2xx and follows no redirect, so a request that ends in a
// 303, a 429, or a 500 does nothing at all on the page — and the reader clicks again,
// which is the worst possible answer to a spent budget. The rewrite lives in
// middleware because each of those is raised where no handler holds a header: inside
// the panic recovery, inside the deadline, inside the token check.
//
// It acts only on requests carrying HX-Request, and only on statuses that have a
// registered Rewriter; everything else streams through untouched. Redirect statuses
// (301, 302, 303, 307, 308) are registered by default and become 200 plus
// HX-Redirect. Register the rest yourself, because the body htmx swaps is a view
// concern this package does not own:
//
//	audible := htmx.NewAudible(
//		htmx.WithRewriter(http.StatusTooManyRequests, toast("slow down for a moment")),
//		htmx.WithRewriter(http.StatusInternalServerError, toast("something went wrong")),
//	)
//	handler := middleware.Wrap(mux, audible, recoverer.New())
//
// A 404 is deliberately not registered by default: it is a page, and a page is what
// the reader should get.
//
// A rewritten response reports 200 to whatever runs outside this middleware, so mount
// request logging and metrics inside it to record the status the handler produced.
//
// Buffering is what makes the rewrite possible, so a handler under this middleware
// cannot stream: it holds the body until the handler returns. Mount it only on the
// routes that answer htmx, never around an SSE or download route.
func NewAudible(opts ...AudibleOption) func(http.Handler) http.Handler {
	c := audibleConfig{byStatus: map[int]Rewriter{
		http.StatusMovedPermanently:  redirectRewriter,
		http.StatusFound:             redirectRewriter,
		http.StatusSeeOther:          redirectRewriter,
		http.StatusTemporaryRedirect: redirectRewriter,
		http.StatusPermanentRedirect: redirectRewriter,
	}}
	for _, opt := range opts {
		opt(&c)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !IsRequest(r) {
				next.ServeHTTP(w, r)
				return
			}
			buffered := &bufferedWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(buffered, r)
			buffered.flush(r, c.byStatus)
		})
	}
}

// redirectRewriter moves the destination from Location to HX-Redirect and answers
// 200, because htmx follows no redirect of its own.
func redirectRewriter(w http.ResponseWriter, _ *http.Request, status int, body []byte) {
	location := w.Header().Get("Location")
	if location == "" {
		passThrough(w, status, body)
		return
	}
	w.Header().Del("Location")
	w.Header().Set(hdrRedirect, location)
	w.WriteHeader(http.StatusOK)
}

// Toast returns a Rewriter that answers 200 with an out-of-band fragment and
// HX-Reswap: none, so the message lands in the toaster and no other element changes.
// The fragment must carry hx-swap-oob="true" and the id of the toaster element.
func Toast(fragment []byte) Rewriter {
	return func(w http.ResponseWriter, _ *http.Request, _ int, _ []byte) {
		header := w.Header()
		header.Set("Content-Type", "text/html; charset=utf-8")
		header.Set("Content-Length", strconv.Itoa(len(fragment)))
		header.Set(hdrReswap, string(SwapNone))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(fragment)
	}
}

// bufferedWriter holds the status and body until the handler returns, so a rewriter
// can replace both. Only the first WriteHeader wins, matching net/http.
type bufferedWriter struct {
	http.ResponseWriter
	body   bytes.Buffer
	status int
	wrote  bool
}

// Unwrap exposes the real writer so http.ResponseController can reach Flush,
// SetWriteDeadline, and Hijack through the buffer.
func (b *bufferedWriter) Unwrap() http.ResponseWriter { return b.ResponseWriter }

func (b *bufferedWriter) WriteHeader(status int) {
	if !b.wrote {
		b.status = status
		b.wrote = true
	}
}

func (b *bufferedWriter) Write(p []byte) (int, error) {
	b.wrote = true
	return b.body.Write(p)
}

func (b *bufferedWriter) flush(r *http.Request, byStatus map[int]Rewriter) {
	if rw, ok := byStatus[b.status]; ok {
		rw(b.ResponseWriter, r, b.status, b.body.Bytes())
		return
	}
	passThrough(b.ResponseWriter, b.status, b.body.Bytes())
}

func passThrough(w http.ResponseWriter, status int, body []byte) {
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
