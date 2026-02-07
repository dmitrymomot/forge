package internal

import (
	"fmt"
	"strings"
)

// ExtractorSource extracts a value from the request context.
// Returns the value and true if found, or ("", false) if not present.
type ExtractorSource = func(Context) (string, bool)

// Extractor tries multiple sources in order and returns the first match.
type Extractor struct {
	sources []ExtractorSource
}

// NewExtractor creates an Extractor that tries the given sources in order.
func NewExtractor(sources ...ExtractorSource) Extractor {
	return Extractor{sources: sources}
}

// Extract iterates sources in order and returns the first non-empty value.
// Returns ("", false) if all sources miss.
func (e Extractor) Extract(c Context) (string, bool) {
	for _, src := range e.sources {
		if v, ok := src(c); ok && v != "" {
			return v, true
		}
	}
	return "", false
}

// FromHeader returns a source that reads from a request header.
func FromHeader(name string) ExtractorSource {
	return func(c Context) (string, bool) {
		v := c.Header(name)
		if v == "" {
			return "", false
		}
		return v, true
	}
}

// FromQuery returns a source that reads from a query parameter.
func FromQuery(name string) ExtractorSource {
	return func(c Context) (string, bool) {
		v := c.Query(name)
		if v == "" {
			return "", false
		}
		return v, true
	}
}

// FromCookie returns a source that reads from a plain cookie.
func FromCookie(name string) ExtractorSource {
	return func(c Context) (string, bool) {
		v, err := c.Cookie(name)
		if err != nil || v == "" {
			return "", false
		}
		return v, true
	}
}

// FromCookieSigned returns a source that reads from a signed cookie.
func FromCookieSigned(name string) ExtractorSource {
	return func(c Context) (string, bool) {
		v, err := c.CookieSigned(name)
		if err != nil || v == "" {
			return "", false
		}
		return v, true
	}
}

// FromCookieEncrypted returns a source that reads from an encrypted cookie.
func FromCookieEncrypted(name string) ExtractorSource {
	return func(c Context) (string, bool) {
		v, err := c.CookieEncrypted(name)
		if err != nil || v == "" {
			return "", false
		}
		return v, true
	}
}

// FromParam returns a source that reads from a URL parameter.
func FromParam(name string) ExtractorSource {
	return func(c Context) (string, bool) {
		v := c.Param(name)
		if v == "" {
			return "", false
		}
		return v, true
	}
}

// FromForm returns a source that reads from a form field.
func FromForm(name string) ExtractorSource {
	return func(c Context) (string, bool) {
		v := c.Form(name)
		if v == "" {
			return "", false
		}
		return v, true
	}
}

// FromSession returns a source that reads from a session value.
// Tries string type assertion first, falls back to fmt.Sprint for non-string values.
func FromSession(key string) ExtractorSource {
	return func(c Context) (string, bool) {
		val, err := c.SessionValue(key)
		if err != nil || val == nil {
			return "", false
		}
		if s, ok := val.(string); ok {
			if s == "" {
				return "", false
			}
			return s, true
		}
		s := fmt.Sprint(val)
		if s == "" {
			return "", false
		}
		return s, true
	}
}

// FromBearerToken returns a source that reads a Bearer token from the Authorization header.
// Uses case-insensitive comparison on the "Bearer " prefix.
func FromBearerToken() ExtractorSource {
	return func(c Context) (string, bool) {
		auth := c.Header("Authorization")
		if len(auth) < 7 || !strings.EqualFold(auth[:7], "bearer ") {
			return "", false
		}
		token := auth[7:]
		if token == "" {
			return "", false
		}
		return token, true
	}
}
