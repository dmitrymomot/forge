package handlers

import (
	"errors"

	"github.com/dmitrymomot/forge"
)

// errTest is a test error for demonstrating error handling.
var errTest = errors.New("this is a test error to verify error handling")

// TestHandler provides test endpoints for verifying error handling.
type TestHandler struct{}

// NewTestHandler creates a new test handler.
func NewTestHandler() *TestHandler {
	return &TestHandler{}
}

// Routes declares test routes.
func (h *TestHandler) Routes(r forge.Router) {
	r.GET("/test/error", h.testError)
}

// testError returns an error to test the global error handler.
func (h *TestHandler) testError(c forge.Context) error {
	return errTest
}
