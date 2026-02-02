package handlers

import (
	"net/http"

	"github.com/dmitrymomot/forge"
)

// Tenant represents a tenant in the system.
type Tenant struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description"`
	URL         string `json:"url"`
}

// Static list of demo tenants (no database needed for this example).
var demoTenants = []Tenant{
	{Slug: "acme", Name: "Acme Corp", Description: "A fictional company for testing", URL: "http://acme.lvh.me:8081"},
	{Slug: "demo", Name: "Demo Inc", Description: "Another demo tenant", URL: "http://demo.lvh.me:8081"},
	{Slug: "test", Name: "Test Co", Description: "For testing purposes", URL: "http://test.lvh.me:8081"},
}

// APIHandler serves the API endpoints.
// Implements forge.Handler interface.
type APIHandler struct{}

// NewAPIHandler creates a new API handler.
func NewAPIHandler() *APIHandler {
	return &APIHandler{}
}

// Routes declares all routes for the API handler.
func (h *APIHandler) Routes(r forge.Router) {
	r.GET("/health", h.health)
	r.GET("/tenants", h.listTenants)
	r.GET("/tenants/{slug}", h.getTenant)
}

// health returns the API health status.
func (h *APIHandler) health(c forge.Context) error {
	return c.JSON(http.StatusOK, map[string]string{
		"status": "ok",
	})
}

// listTenants returns a list of all tenants.
func (h *APIHandler) listTenants(c forge.Context) error {
	return c.JSON(http.StatusOK, map[string]any{
		"tenants": demoTenants,
		"total":   len(demoTenants),
	})
}

// getTenant returns a specific tenant by slug.
func (h *APIHandler) getTenant(c forge.Context) error {
	slug := c.Param("slug")

	for _, t := range demoTenants {
		if t.Slug == slug {
			return c.JSON(http.StatusOK, t)
		}
	}

	return c.JSON(http.StatusNotFound, map[string]string{
		"error": "tenant not found",
	})
}
