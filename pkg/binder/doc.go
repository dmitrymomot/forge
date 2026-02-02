// Package binder provides comprehensive HTTP request data binding utilities for Go web applications.
// It supports binding JSON, form data, query parameters, and path parameters to Go structs
// with built-in validation, sanitization, and security features.
//
// # Features
//
//   - JSON binding with strict parsing and size limits
//   - Form data binding supporting both URL-encoded and multipart forms
//   - Query parameter binding with multi-value support
//   - Path parameter binding compatible with popular routers
//   - Automatic input sanitization to prevent XSS and injection attacks
//   - Comprehensive error handling with descriptive messages
//   - Security hardening against DoS and malformed data attacks
//
// # Usage
//
// The package provides four main binding functions that can be used individually
// or combined:
//
//	import "github.com/dmitrymomot/forge/pkg/binder"
//
//	// JSON binding
//	jsonBinder := binder.JSON()
//
//	// Form binding (URL-encoded and multipart)
//	formBinder := binder.Form()
//
//	// Query parameter binding
//	queryBinder := binder.Query()
//
//	// Path parameter binding (requires router-specific extractor)
//	pathBinder := binder.Path(chi.URLParam) // for chi router
//
// # JSON Binding
//
// JSON binding parses request bodies with Content-Type validation, size limits,
// and strict parsing to prevent malformed data:
//
//	type CreateUserRequest struct {
//		Name     string `json:"name"`
//		Email    string `json:"email"`
//		Age      int    `json:"age"`
//		Optional *bool  `json:"optional,omitempty"`
//	}
//
//	func handler(w http.ResponseWriter, r *http.Request) {
//		var req CreateUserRequest
//		if err := binder.JSON()(r, &req); err != nil {
//			http.Error(w, err.Error(), http.StatusBadRequest)
//			return
//		}
//		// req is now populated from JSON body
//	}
//
// # Form Binding
//
// Form binding handles both URL-encoded forms and multipart forms with file uploads.
// It supports comprehensive struct tags and type conversion:
//
//	type UploadRequest struct {
//		Title       string                  `form:"title"`
//		Category    string                  `form:"category"`
//		Tags        []string                `form:"tags"`     // Multi-value support
//		IsPublic    bool                    `form:"public"`   // String to bool conversion
//		Priority    int                     `form:"priority"` // String to int conversion
//		Avatar      *multipart.FileHeader   `file:"avatar"`   // Single file upload
//		Attachments []*multipart.FileHeader `file:"files"`    // Multiple file uploads
//		Internal    string                  `form:"-"`        // Ignored field
//	}
//
//	func uploadHandler(w http.ResponseWriter, r *http.Request) {
//		var req UploadRequest
//		if err := binder.Form()(r, &req); err != nil {
//			http.Error(w, err.Error(), http.StatusBadRequest)
//			return
//		}
//
//		// Process uploaded files
//		if req.Avatar != nil {
//			file, err := req.Avatar.Open()
//			if err != nil {
//				http.Error(w, "Failed to open file", http.StatusInternalServerError)
//				return
//			}
//			defer file.Close()
//			// Process file content
//		}
//	}
//
// # Query Parameter Binding
//
// Query parameter binding extracts data from URL query strings with support
// for multi-value parameters and type conversion:
//
//	type SearchRequest struct {
//		Query     string   `query:"q"`
//		Page      int      `query:"page"`
//		PageSize  int      `query:"page_size"`
//		Tags      []string `query:"tags"`     // ?tags=go&tags=web
//		Active    *bool    `query:"active"`   // Optional parameter
//		SortBy    string   `query:"sort"`
//		Ascending bool     `query:"asc"`
//	}
//
//	func searchHandler(w http.ResponseWriter, r *http.Request) {
//		var req SearchRequest
//		if err := binder.Query()(r, &req); err != nil {
//			http.Error(w, err.Error(), http.StatusBadRequest)
//			return
//		}
//		// req is populated from query parameters
//	}
//
// # Path Parameter Binding
//
// Path parameter binding extracts values from URL path segments using
// router-specific extractor functions:
//
//	import "github.com/go-chi/chi/v5"
//
//	type ProfileRequest struct {
//		UserID   string `path:"id"`
//		Username string `path:"username"`
//	}
//
//	func main() {
//		r := chi.NewRouter()
//		pathBinder := binder.Path(chi.URLParam)
//
//		r.Get("/users/{id}/profile/{username}", func(w http.ResponseWriter, r *http.Request) {
//			var req ProfileRequest
//			if err := pathBinder(r, &req); err != nil {
//				http.Error(w, err.Error(), http.StatusBadRequest)
//				return
//			}
//			// req.UserID and req.Username are populated from path
//		})
//	}
//
//	// With gorilla/mux
//	import "github.com/gorilla/mux"
//
//	func setupMuxRoutes() {
//		muxExtractor := func(r *http.Request, fieldName string) string {
//			vars := mux.Vars(r)
//			return vars[fieldName]
//		}
//		pathBinder := binder.Path(muxExtractor)
//
//		router := mux.NewRouter()
//		router.HandleFunc("/users/{id}/profile/{username}", func(w http.ResponseWriter, r *http.Request) {
//			var req ProfileRequest
//			if err := pathBinder(r, &req); err != nil {
//				http.Error(w, err.Error(), http.StatusBadRequest)
//				return
//			}
//			// Use req...
//		})
//	}
//
// # Combining Multiple Binders
//
// Multiple binders can be combined to handle complex request structures
// that include data from different sources:
//
//	type ComplexRequest struct {
//		// From path parameters
//		UserID   string `path:"id"`
//
//		// From query parameters
//		Page     int    `query:"page"`
//		PageSize int    `query:"page_size"`
//
//		// From form data
//		Name     string                `form:"name"`
//		Avatar   *multipart.FileHeader `file:"avatar"`
//	}
//
//	func complexHandler(w http.ResponseWriter, r *http.Request) {
//		var req ComplexRequest
//
//		// Apply binders in sequence
//		binders := []func(*http.Request, any) error{
//			binder.Path(chi.URLParam),
//			binder.Query(),
//			binder.Form(),
//		}
//
//		for _, bind := range binders {
//			if err := bind(r, &req); err != nil {
//				http.Error(w, err.Error(), http.StatusBadRequest)
//				return
//			}
//		}
//
//		// req is now populated from all sources
//	}
//
// # Supported Types
//
// The binder package supports automatic type conversion for:
//
//   - string
//   - int, int8, int16, int32, int64
//   - uint, uint8, uint16, uint32, uint64
//   - float32, float64
//   - bool (recognizes: true, false, 1, 0, on, off, yes, no)
//   - Slices of any of the above types
//   - Pointers to any of the above types (for optional fields)
//   - *multipart.FileHeader and []*multipart.FileHeader (for file uploads)
//
// # Security Features
//
// The package includes several security hardening measures:
//
//   - Request size limits to prevent DoS attacks (DefaultMaxJSONSize=1MB, DefaultMaxMemory=10MB)
//   - Input sanitization to prevent XSS and injection attacks
//   - Filename sanitization for uploaded files to prevent path traversal
//   - Boundary validation for multipart forms
//   - Strict JSON parsing with unknown field rejection
//   - Context timeout handling to avoid processing cancelled requests
//
// # Error Handling
//
// The package provides comprehensive error types for different failure scenarios:
//
//	import (
//		"errors"
//		"github.com/dmitrymomot/forge/pkg/binder"
//	)
//
//	func handleBindingError(err error) {
//		switch {
//		case errors.Is(err, binder.ErrUnsupportedMediaType):
//			// Handle unsupported media type
//		case errors.Is(err, binder.ErrFailedToParseJSON):
//			// Handle JSON parsing error
//		case errors.Is(err, binder.ErrFailedToParseForm):
//			// Handle form parsing error
//		case errors.Is(err, binder.ErrFailedToParseQuery):
//			// Handle query parsing error
//		case errors.Is(err, binder.ErrFailedToParsePath):
//			// Handle path parsing error
//		case errors.Is(err, binder.ErrMissingContentType):
//			// Handle missing content type
//		case errors.Is(err, binder.ErrBinderNotApplicable):
//			// Handle inapplicable binder
//		default:
//			// Handle other binding errors
//		}
//	}
//
// # Constants
//
// The package defines the following constants:
//
//   - DefaultMaxJSONSize: Maximum JSON request body size (1MB)
//   - DefaultMaxMemory: Maximum memory for multipart form parsing (10MB)
package binder
