package idempotency

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/dmitrymomot/forge/resilience/cache"
	"github.com/dmitrymomot/forge/web/middleware"
	"github.com/dmitrymomot/forge/web/problem"
)

func defaultMethods() map[string]bool {
	return map[string]bool{
		http.MethodPost:   true,
		http.MethodPut:    true,
		http.MethodPatch:  true,
		http.MethodDelete: true,
	}
}

// New returns Idempotency-Key middleware backed by store. On a guarded method,
// the first request with a given key atomically claims it and its response
// (status < 500) is stored and replayed to later retries; a concurrent duplicate
// gets 409; the same key with a different payload gets 422; a 5xx releases the
// claim so a genuine retry re-executes. The memory cache.Store is LRU-evicting
// and unsuitable in production — use cache/redis or another durable Store.
func New(store cache.Store, opts ...Option) middleware.Middleware {
	cfg := config{
		header:        "Idempotency-Key",
		methods:       defaultMethods(),
		ttl:           24 * time.Hour,
		processingTTL: time.Minute,
		maxBody:       1 << 20,
	}
	for _, o := range opts {
		o(&cfg)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !cfg.methods[r.Method] {
				next.ServeHTTP(w, r)
				return
			}
			key := r.Header.Get(cfg.header)
			if key == "" {
				if cfg.requireKey {
					reject(w, r, ErrKeyRequired)
					return
				}
				next.ServeHTTP(w, r)
				return
			}

			body, tooBig, err := readCapped(r, cfg.maxBody)
			if err != nil {
				reject(w, r, ErrReadBody)
				return
			}
			if tooBig {
				reject(w, r, ErrRequestTooLarge)
				return
			}
			fp := fingerprint(r.Method, r.URL.Path, body)

			ctx := r.Context()
			err = store.Set(ctx, key, encodeProcessing(), cache.WithSetNonExist(), cache.WithTTL(cfg.processingTTL))
			switch {
			case errors.Is(err, cache.ErrExists):
				handleExisting(w, r, store, key, fp)
				return
			case err != nil:
				// Store unavailable: cannot guarantee idempotency; execute once.
				next.ServeHTTP(w, r)
				return
			}

			cw := &capture{ResponseWriter: w, limit: cfg.maxBody}
			completed := false
			defer func() {
				if !completed {
					// panic before completion — release the claim; the panic then propagates.
					_ = store.Delete(context.WithoutCancel(ctx), key)
				}
			}()
			next.ServeHTTP(cw, r)
			completed = true

			status := cw.finalStatus()
			switch {
			case cw.over:
				// Too large to cache; response already streamed to the client.
				_ = store.Delete(context.WithoutCancel(ctx), key)
			case status >= 200 && status < 500:
				// Deterministic outcome (2xx/3xx/4xx) — freeze and replay.
				rec := encodeDone(fp, status, filterHeader(cw.Header()), cw.buf.Bytes())
				_ = store.Set(ctx, key, rec, cache.WithTTL(cfg.ttl))
				cw.flush()
			default:
				// 5xx (or 1xx) — release so a retry actually re-executes.
				_ = store.Delete(context.WithoutCancel(ctx), key)
				cw.flush()
			}
		})
	}
}

func handleExisting(w http.ResponseWriter, r *http.Request, store cache.Store, key string, fp [32]byte) {
	data, err := store.Get(r.Context(), key)
	if err != nil {
		// Marker vanished between Set and Get (expiry/race) — safest answer.
		reject(w, r, ErrInProgress)
		return
	}
	rec, err := decode(data)
	if err != nil || rec.kind == kindProcessing {
		reject(w, r, ErrInProgress)
		return
	}
	if rec.fp != fp {
		reject(w, r, ErrKeyReuse)
		return
	}
	replay(w, rec)
}

func replay(w http.ResponseWriter, rec stored) {
	h := w.Header()
	for k, vs := range rec.header {
		for _, v := range vs {
			h.Add(k, v)
		}
	}
	w.WriteHeader(rec.status)
	_, _ = w.Write(rec.body)
}

func fingerprint(method, path string, body []byte) [32]byte {
	h := sha256.New()
	_, _ = h.Write([]byte(method))
	_, _ = h.Write([]byte{'\n'})
	_, _ = h.Write([]byte(path))
	_, _ = h.Write([]byte{'\n'})
	_, _ = h.Write(body)
	var out [32]byte
	h.Sum(out[:0])
	return out
}

// readCapped reads up to limit bytes of the request body for fingerprinting and
// restores r.Body for the handler. tooBig is true when the body exceeds limit.
func readCapped(r *http.Request, limit int64) (body []byte, tooBig bool, err error) {
	if r.Body == nil {
		return nil, false, nil
	}
	buf, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(buf)) > limit {
		return nil, true, nil
	}
	r.Body = io.NopCloser(bytes.NewReader(buf))
	return buf, false, nil
}

// hopByHop headers plus Set-Cookie are never stored or replayed. Replaying a
// rotated auth cookie to a later retry would be unsafe.
var hopByHop = map[string]struct{}{
	"Connection":          {},
	"Keep-Alive":          {},
	"Proxy-Authenticate":  {},
	"Proxy-Authorization": {},
	"Te":                  {},
	"Trailer":             {},
	"Transfer-Encoding":   {},
	"Upgrade":             {},
	"Set-Cookie":          {},
}

func filterHeader(src http.Header) http.Header {
	out := make(http.Header, len(src))
	for k, vs := range src {
		if _, skip := hopByHop[http.CanonicalHeaderKey(k)]; skip {
			continue
		}
		out[k] = append([]string(nil), vs...)
	}
	return out
}

func reject(w http.ResponseWriter, r *http.Request, err error) {
	problem.JSON(problem.WithStatusOf(statusOf))(w, r, err)
}

func statusOf(err error) int {
	switch {
	case errors.Is(err, ErrKeyRequired), errors.Is(err, ErrReadBody):
		return http.StatusBadRequest
	case errors.Is(err, ErrInProgress):
		return http.StatusConflict
	case errors.Is(err, ErrKeyReuse):
		return http.StatusUnprocessableEntity
	case errors.Is(err, ErrRequestTooLarge):
		return http.StatusRequestEntityTooLarge
	default:
		return http.StatusInternalServerError
	}
}
