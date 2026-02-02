package main

import (
	"net/http"

	"github.com/dmitrymomot/forge"
)

// --- Landing Handler ---

type landingHandler struct{}

func (h *landingHandler) Routes(r forge.Router) {
	r.GET("/", func(c forge.Context) error {
		return c.String(http.StatusOK, "Landing: Home")
	})
	r.GET("/about", func(c forge.Context) error {
		return c.String(http.StatusOK, "Landing: About")
	})
}

// --- API Handler ---

type apiHandler struct{}

func (h *apiHandler) Routes(r forge.Router) {
	r.GET("/health", func(c forge.Context) error {
		return c.String(http.StatusOK, "API: OK")
	})
	r.GET("/users/{id}", func(c forge.Context) error {
		return c.String(http.StatusOK, "API: User "+c.Param("id"))
	})
}

// --- Tenant Handler ---

type tenantHandler struct{}

func (h *tenantHandler) Routes(r forge.Router) {
	r.GET("/", func(c forge.Context) error {
		tenant := forge.ContextValue[string](c, tenantKey{})
		return c.String(http.StatusOK, "Tenant ["+tenant+"]: Dashboard")
	})
	r.GET("/settings", func(c forge.Context) error {
		tenant := forge.ContextValue[string](c, tenantKey{})
		return c.String(http.StatusOK, "Tenant ["+tenant+"]: Settings")
	})
}
