package cors

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/dmitrymomot/forge/web/middleware"
)

// New returns CORS middleware. Requests without an Origin header pass
// untouched. Preflights are answered directly with 204; CORS headers are
// emitted only for allowed origins — disallowed requests are still served
// (the browser enforces the policy), matching CORS semantics.
func New(opts ...Option) (middleware.Middleware, error) {
	cf := config{cfg: DefaultConfig()}
	for _, o := range opts {
		o(&cf)
	}
	if err := cf.cfg.Validate(); err != nil {
		return nil, err
	}
	allowAll := false
	var rules []originRule
	for _, o := range cf.cfg.AllowedOrigins {
		if o == "*" {
			allowAll = true
			continue
		}
		rule, err := parseOrigin(o)
		if err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	allowed := func(origin string) bool {
		if cf.originFn != nil {
			return cf.originFn(origin)
		}
		if allowAll {
			return true
		}
		for _, r := range rules {
			if r.match(origin) {
				return true
			}
		}
		return false
	}
	methods := strings.Join(cf.cfg.AllowedMethods, ", ")
	headers := strings.Join(cf.cfg.AllowedHeaders, ", ")
	exposed := strings.Join(cf.cfg.ExposedHeaders, ", ")
	maxAge := ""
	if cf.cfg.MaxAge > 0 {
		maxAge = strconv.FormatInt(int64(cf.cfg.MaxAge.Seconds()), 10)
	}
	credentials := cf.cfg.AllowCredentials
	// ACAO "*" is only valid without credentials and only for the bare
	// wildcard rule; every other allowance echoes the origin.
	star := allowAll && !credentials && cf.originFn == nil

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}
			h := w.Header()
			h.Add("Vary", "Origin")
			if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
				h.Add("Vary", "Access-Control-Request-Method")
				h.Add("Vary", "Access-Control-Request-Headers")
				if allowed(origin) {
					setOrigin(h, origin, star)
					if credentials {
						h.Set("Access-Control-Allow-Credentials", "true")
					}
					h.Set("Access-Control-Allow-Methods", methods)
					switch {
					case headers != "":
						h.Set("Access-Control-Allow-Headers", headers)
					default:
						if rh := r.Header.Get("Access-Control-Request-Headers"); rh != "" {
							h.Set("Access-Control-Allow-Headers", rh)
						}
					}
					if maxAge != "" {
						h.Set("Access-Control-Max-Age", maxAge)
					}
				}
				w.WriteHeader(http.StatusNoContent)
				return
			}
			if allowed(origin) {
				setOrigin(h, origin, star)
				if credentials {
					h.Set("Access-Control-Allow-Credentials", "true")
				}
				if exposed != "" {
					h.Set("Access-Control-Expose-Headers", exposed)
				}
			}
			next.ServeHTTP(w, r)
		})
	}, nil
}

func setOrigin(h http.Header, origin string, star bool) {
	if star {
		h.Set("Access-Control-Allow-Origin", "*")
		return
	}
	h.Set("Access-Control-Allow-Origin", origin)
}
