package handlers

import (
	"net/http"

	"github.com/dmitrymomot/forge"
	"github.com/dmitrymomot/forge/examples/multi-domain/views"
)

// LandingHandler serves the main marketing site pages.
// Implements forge.Handler interface.
type LandingHandler struct{}

// NewLandingHandler creates a new landing handler.
func NewLandingHandler() *LandingHandler {
	return &LandingHandler{}
}

// Routes declares all routes for the landing handler.
func (h *LandingHandler) Routes(r forge.Router) {
	r.GET("/", h.home)
	r.GET("/features", h.features)
	r.GET("/pricing", h.pricing)
}

// home renders the landing page.
func (h *LandingHandler) home(c forge.Context) error {
	return c.Render(http.StatusOK, views.HomePage())
}

// features renders the features page.
func (h *LandingHandler) features(c forge.Context) error {
	return c.Render(http.StatusOK, views.FeaturesPage())
}

// pricing renders the pricing page.
func (h *LandingHandler) pricing(c forge.Context) error {
	return c.Render(http.StatusOK, views.PricingPage())
}
