// Package htmx provides utilities for working with HTMX requests and responses.
//
// HTMX enables developers to access AJAX, WebSockets, and Server-Sent Events
// directly in HTML attributes, simplifying dynamic web application development.
// This package offers convenient functions and constants to detect HTMX requests
// and send appropriate HTMX response headers.
//
// # Request Detection
//
// Use IsHTMX to check if an incoming HTTP request originated from an HTMX element:
//
//	import (
//		"github.com/dmitrymomot/boilerplate/pkg/htmx"
//		"net/http"
//	)
//
//	func myHandler(w http.ResponseWriter, r *http.Request) {
//		if htmx.IsHTMX(r) {
//			// Handle HTMX-specific logic
//		}
//	}
//
// # Response Headers
//
// The package exports constants for all HTMX response headers. Common headers include:
//   - HX-Location: Client-side navigation with URL update
//   - HX-Redirect: Client-side redirect
//   - HX-Retarget: Change the target element
//   - HX-Reswap: Change the swap strategy
//   - HX-Refresh: Refresh the page
//
// # Navigation and Redirects
//
// Use the Location and Redirect functions to handle navigation for both HTMX
// and regular HTTP requests. These functions automatically detect HTMX requests
// and respond appropriately:
//
//	// Simple redirect - uses HX-Redirect header for HTMX, HTTP redirect for regular requests
//	htmx.Redirect(w, r, "/new-page")
//
//	// Navigation with target element update
//	htmx.LocationTarget(w, r, "/api/users", "#user-list")
//
//	// Advanced location options with custom parameters
//	opts := htmx.LocationOptions{
//		Path:   "/dashboard",
//		Target: "#main",
//		Swap:   string(htmx.SwapInnerHTML),
//	}
//	htmx.LocationWithOptions(w, r, opts)
//
// # Swap Strategies
//
// The SwapStrategy type defines how content should be inserted into the target element:
//   - SwapInnerHTML: Replace inner HTML (default)
//   - SwapOuterHTML: Replace entire element
//   - SwapBeforeBegin: Insert before element
//   - SwapAfterBegin: Insert before first child
//   - SwapBeforeEnd: Insert after last child
//   - SwapAfterEnd: Insert after element
//   - SwapDelete: Remove the element
//   - SwapNone: Don't swap
package htmx
