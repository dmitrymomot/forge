package binder

import "net/http"

// Binder represents a function that binds HTTP request data to a Go value.
// It provides a unified interface for extracting and mapping data from various
// parts of an HTTP request (form data, JSON body, path parameters, query parameters)
// into strongly-typed Go structures.
type Binder func(r *http.Request, v any) error
