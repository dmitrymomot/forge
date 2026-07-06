package secheaders

import (
	"context"
	"net/http"
	"strconv"

	"github.com/dmitrymomot/forge/core/ctxkey"
	"github.com/dmitrymomot/forge/core/random"
	"github.com/dmitrymomot/forge/web/middleware"
)

var nonceKey = ctxkey.New[string]("csp_nonce")

// nonceBytes is the entropy of a CSP nonce (base64url-encoded per request).
const nonceBytes = 16

// New returns middleware that sets security headers on every response.
// Headers already set by earlier middleware are left alone, and handlers can
// overwrite anything later — handler wins.
func New(opts ...Option) (middleware.Middleware, error) {
	cf := config{cfg: DefaultConfig()}
	for _, o := range opts {
		o(&cf)
	}
	if err := cf.cfg.Validate(); err != nil {
		return nil, err
	}
	frameOptions := cf.cfg.FrameOptions
	if frameOptions == "" {
		frameOptions = "DENY"
	}
	hsts := ""
	if cf.cfg.HSTSMaxAge > 0 {
		hsts = "max-age=" + strconv.FormatInt(int64(cf.cfg.HSTSMaxAge.Seconds()), 10)
		if cf.cfg.HSTSIncludeSubdomains {
			hsts += "; includeSubDomains"
		}
	}
	if cf.policy != nil && cf.cfg.CSPReportURI != "" && cf.policy.ReportURI == "" {
		cf.policy.ReportURI = cf.cfg.CSPReportURI
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			setIfEmpty(h, "X-Content-Type-Options", "nosniff")
			setIfEmpty(h, "Referrer-Policy", "strict-origin-when-cross-origin")
			setIfEmpty(h, "Cross-Origin-Opener-Policy", "same-origin")
			if frameOptions != "off" {
				setIfEmpty(h, "X-Frame-Options", frameOptions)
			}
			if hsts != "" {
				setIfEmpty(h, "Strict-Transport-Security", hsts)
			}
			nonce := ""
			if cf.nonce {
				nonce = random.URLSafe(nonceBytes)
				r = r.WithContext(nonceKey.With(r.Context(), nonce))
			}
			if cf.policy != nil {
				setIfEmpty(h, "Content-Security-Policy", cf.policy.render(nonce))
			}
			next.ServeHTTP(w, r)
		})
	}, nil
}

// Nonce returns the per-request CSP nonce for template use ("" when nonce
// generation is disabled or outside the middleware).
func Nonce(ctx context.Context) string {
	v, _ := nonceKey.From(ctx)
	return v
}

func setIfEmpty(h http.Header, key, value string) {
	if h.Get(key) == "" {
		h.Set(key, value)
	}
}
