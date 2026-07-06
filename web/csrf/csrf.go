package csrf

import (
	"crypto/subtle"
	"mime"
	"net/http"

	"github.com/dmitrymomot/forge/core/ctxkey"
	"github.com/dmitrymomot/forge/core/random"
	"github.com/dmitrymomot/forge/web/cookie"
	"github.com/dmitrymomot/forge/web/middleware"
	"github.com/dmitrymomot/forge/web/problem"
)

var tokenKey = ctxkey.New[string]("csrf_token")

// tokenBytes is the entropy of a minted token (32 bytes, base64url-encoded).
const tokenBytes = 32

// New returns stateless double-submit CSRF middleware over codec. The token
// lives in a signed cookie; unsafe methods must echo it via header or form
// field. New panics if codec is nil — that is a wiring bug, not a runtime
// condition.
func New(codec *cookie.Codec, opts ...Option) middleware.Middleware {
	if codec == nil {
		panic("csrf: nil cookie codec")
	}
	cfg := config{
		cookieName: "__Host-csrf",
		header:     "X-CSRF-Token",
		formField:  "csrf_token",
		responder:  problem.JSON(problem.WithStatus(http.StatusForbidden)),
	}
	for _, o := range opts {
		o(&cfg)
	}
	if cfg.cookieName == "__Host-csrf" && !codec.SupportsHostPrefix() {
		cfg.cookieName = "csrf"
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if cfg.skip != nil && cfg.skip(r) {
				next.ServeHTTP(w, r)
				return
			}
			token, err := codec.GetSigned(r, cfg.cookieName)
			fresh := err != nil
			if fresh {
				token = random.URLSafe(tokenBytes)
				if werr := codec.SetSigned(w, cfg.cookieName, token); werr != nil {
					cfg.responder(w, r, werr)
					return
				}
			}
			r = r.WithContext(tokenKey.With(r.Context(), token))
			if safeMethod(r.Method) {
				next.ServeHTTP(w, r)
				return
			}
			if fresh {
				// Unsafe request without a pre-existing valid cookie can never
				// have a matching echo.
				cfg.responder(w, r, ErrTokenMissing)
				return
			}
			echo := r.Header.Get(cfg.header)
			if echo == "" && isForm(r) {
				echo = r.PostFormValue(cfg.formField)
			}
			switch {
			case echo == "":
				cfg.responder(w, r, ErrTokenMissing)
			case subtle.ConstantTimeCompare([]byte(echo), []byte(token)) != 1:
				cfg.responder(w, r, ErrTokenInvalid)
			default:
				next.ServeHTTP(w, r)
			}
		})
	}
}

// Token returns the CSRF token for the current request — put it in a
// <meta name="csrf-token"> tag or an htmx hx-headers attribute. It returns
// "" outside the middleware.
func Token(r *http.Request) string {
	v, _ := tokenKey.From(r.Context())
	return v
}

func safeMethod(m string) bool {
	switch m {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return true
	}
	return false
}

func isForm(r *http.Request) bool {
	ct, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		return false
	}
	return ct == "application/x-www-form-urlencoded" || ct == "multipart/form-data"
}
