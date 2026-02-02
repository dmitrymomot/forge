package handlers

import (
	"net/http"

	"github.com/dmitrymomot/forge"
	"github.com/dmitrymomot/forge/examples/multi-domain/middleware"
	"github.com/dmitrymomot/forge/examples/multi-domain/views"
)

// TenantHandler serves the tenant dashboard pages.
// Implements forge.Handler interface.
type TenantHandler struct{}

// NewTenantHandler creates a new tenant handler.
func NewTenantHandler() *TenantHandler {
	return &TenantHandler{}
}

// Routes declares all routes for the tenant handler.
func (h *TenantHandler) Routes(r forge.Router) {
	r.GET("/", h.dashboard)
	r.GET("/settings", h.settings)
}

// dashboard renders the tenant dashboard page.
func (h *TenantHandler) dashboard(c forge.Context) error {
	tenant := middleware.TenantFromContext(c.Context())
	if tenant == "" {
		return c.Error(http.StatusInternalServerError, "Tenant not found in context")
	}

	return c.Render(http.StatusOK, views.TenantDashboardPage(tenant))
}

// settings renders the tenant settings page.
func (h *TenantHandler) settings(c forge.Context) error {
	tenant := middleware.TenantFromContext(c.Context())
	if tenant == "" {
		return c.Error(http.StatusInternalServerError, "Tenant not found in context")
	}

	return c.Render(http.StatusOK, views.TenantSettingsPage(tenant))
}
