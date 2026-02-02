package main

import (
	"net/http"
	"strings"

	"github.com/dmitrymomot/forge"
)

type tenantKey struct{}

func tenantMiddleware(next forge.HandlerFunc) forge.HandlerFunc {
	return func(c forge.Context) error {
		host := c.Request().Host
		if idx := strings.LastIndex(host, ":"); idx != -1 {
			host = host[:idx]
		}
		parts := strings.Split(host, ".")
		if len(parts) < 3 {
			return c.Error(http.StatusBadRequest, "missing subdomain")
		}
		c.Set(tenantKey{}, strings.ToLower(parts[0]))
		return next(c)
	}
}
