package i18n

import (
	"net/http"
	"slices"
)

// Resolver extracts a locale from a request. It is a struct rather than a bare
// func because Middleware must know which request headers the chain consults
// in order to emit a correct Vary.
type Resolver struct {
	// fn resolves the locale, reporting ok=false to pass to the next resolver.
	fn func(*Bundle, *http.Request) (Locale, bool)
	// vary is the request header this resolver reads, or "" if it reads none.
	vary string
}

// NewResolver builds a custom resolver. fn reports ok=false to pass — for
// example, resolving an authenticated user's saved locale and deferring to the
// rest of the chain for anonymous requests. varyHeader is the request header
// fn reads, or "" for none; Middleware adds it to the response Vary.
func NewResolver(varyHeader string, fn func(b *Bundle, r *http.Request) (Locale, bool)) Resolver {
	return Resolver{fn: fn, vary: varyHeader}
}

// FromCookie resolves the locale from a cookie. A missing or malformed cookie
// falls through rather than erroring.
func FromCookie(name string) Resolver {
	return Resolver{
		vary: "Cookie",
		fn: func(b *Bundle, r *http.Request) (Locale, bool) {
			c, err := r.Cookie(name)
			if err != nil || c.Value == "" {
				return Locale{}, false
			}
			loc, err := b.Parse(c.Value)
			return loc, err == nil
		},
	}
}

// FromQuery resolves the locale from a query parameter. It reads the URL only
// — never ParseForm, which would consume the request body.
func FromQuery(name string) Resolver {
	return Resolver{
		fn: func(b *Bundle, r *http.Request) (Locale, bool) {
			v := r.URL.Query().Get(name)
			if v == "" {
				return Locale{}, false
			}
			loc, err := b.Parse(v)
			return loc, err == nil
		},
	}
}

// FromHeader resolves the locale from a single-value request header, e.g. a
// custom X-Locale.
func FromHeader(name string) Resolver {
	return Resolver{
		vary: name,
		fn: func(b *Bundle, r *http.Request) (Locale, bool) {
			v := r.Header.Get(name)
			if v == "" {
				return Locale{}, false
			}
			loc, err := b.Parse(v)
			return loc, err == nil
		},
	}
}

// FromAcceptLanguage negotiates the locale from the Accept-Language header.
func FromAcceptLanguage() Resolver {
	return Resolver{
		vary: "Accept-Language",
		fn: func(b *Bundle, r *http.Request) (Locale, bool) {
			return b.negotiate(r.Header.Get("Accept-Language"))
		},
	}
}

// defaultChain is cookie → query → Accept-Language, skipping the cookie and
// query resolvers when their Config names are empty.
func (b *Bundle) defaultChain() []Resolver {
	chain := make([]Resolver, 0, 3)
	if b.cfg.CookieName != "" {
		chain = append(chain, FromCookie(b.cfg.CookieName))
	}
	if b.cfg.QueryParam != "" {
		chain = append(chain, FromQuery(b.cfg.QueryParam))
	}
	return append(chain, FromAcceptLanguage())
}

// Middleware resolves the request locale and stamps a Localizer into the
// context, so the package-level one-liners work in every downstream handler.
// It sets Content-Language to the resolved tag and adds each resolver's
// header to the response Vary via Header.Add, so any Vary value already
// present — set by an outer middleware, or by next itself — is preserved
// rather than overwritten.
//
// With no resolvers it uses cookie → query → Accept-Language → default. Any
// explicit chain replaces that order entirely; the bundle default is always
// the final fallback, so resolution never fails.
//
// Vary is computed from the chain's declared headers, not from which resolver
// actually won: if Accept-Language can decide any request, the response
// varies by it, because a request without the cookie would negotiate
// differently — a cache keyed only on the URL would otherwise serve the wrong
// language to a user who never sent that cookie.
//
// The middleware never writes cookies: persisting a user's choice is the
// consumer's concern. It never panics and always calls next exactly once,
// with a context carrying a stamped Localizer.
func (b *Bundle) Middleware(resolvers ...Resolver) func(http.Handler) http.Handler {
	chain := resolvers
	if len(chain) == 0 {
		chain = b.defaultChain()
	}
	vary := make([]string, 0, len(chain))
	for _, res := range chain {
		// Resolver is an exported struct, so a zero value (or NewResolver with
		// a nil fn) can reach here; such a resolver reads no header and can
		// resolve nothing, so it contributes neither a Vary entry below nor a
		// call in the request loop — keeping the "never panics" guarantee.
		if res.fn == nil {
			continue
		}
		if res.vary != "" && !slices.Contains(vary, res.vary) {
			vary = append(vary, res.vary)
		}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			loc := b.Default()
			for _, res := range chain {
				if res.fn == nil {
					continue
				}
				if l, ok := res.fn(b, r); ok {
					loc = l
					break
				}
			}
			h := w.Header()
			for _, v := range vary {
				h.Add("Vary", v)
			}
			h.Set("Content-Language", loc.Tag())
			next.ServeHTTP(w, r.WithContext(b.WithLocale(r.Context(), loc)))
		})
	}
}
