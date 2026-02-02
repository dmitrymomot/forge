package binder

import (
	"fmt"
	"net/http"
	"reflect"
)

// Path creates a path parameter binder function using the provided extractor.
// The extractor function is called for each struct field to get its path parameter value.
//
// It supports struct tags for custom parameter names:
//   - `path:"name"` - binds to path parameter "name"
//   - `path:"-"` - skips the field
//
// Supported types:
//   - Basic types: string, int, int64, uint, uint64, float32, float64, bool
//   - Pointers for optional fields
//
// Example with chi router:
//
//	type ProfileRequest struct {
//		UserID   string `path:"id"`
//		Username string `path:"username"`
//		Name     string `form:"name"`     // From form data
//		Expand   bool   `query:"expand"`  // From query string
//	}
//
//	func profileHandler(w http.ResponseWriter, r *http.Request) {
//		var req ProfileRequest
//
//		// Apply multiple binders in sequence
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
//		// req.UserID and req.Username are populated from path
//		// req.Name is populated from form data
//		// req.Expand is populated from query string
//		// Process req and return response...
//	}
//
//	r := chi.NewRouter()
//	r.Get("/users/{id}/profile/{username}", profileHandler)
//
// Example with gorilla/mux:
//
//	muxExtractor := func(r *http.Request, fieldName string) string {
//		vars := mux.Vars(r)
//		return vars[fieldName]
//	}
//
//	router := mux.NewRouter()
//	router.HandleFunc("/users/{id}/profile/{username}", func(w http.ResponseWriter, r *http.Request) {
//		var req ProfileRequest
//		if err := binder.Path(muxExtractor)(r, &req); err != nil {
//			http.Error(w, err.Error(), http.StatusBadRequest)
//			return
//		}
//		// Process req...
//	})
func Path(extractor func(r *http.Request, fieldName string) string) Binder {
	return func(r *http.Request, v any) error {
		if extractor == nil {
			return fmt.Errorf("%w: extractor function is nil", ErrFailedToParsePath)
		}

		rv := reflect.ValueOf(v)
		if rv.Kind() != reflect.Pointer || rv.IsNil() {
			return fmt.Errorf("%w: target must be a non-nil pointer", ErrFailedToParsePath)
		}

		rv = rv.Elem()
		if rv.Kind() != reflect.Struct {
			return fmt.Errorf("%w: target must be a pointer to struct", ErrFailedToParsePath)
		}

		rt := rv.Type()

		for i := range rv.NumField() {
			field := rv.Field(i)
			fieldType := rt.Field(i)

			// Skip unexported fields that reflection cannot modify
			if !field.CanSet() {
				continue
			}

			paramName, skip := parseFieldTag(fieldType, "path")
			if skip {
				continue
			}

			value := extractor(r, paramName)
			if value == "" {
				continue // Leave field as zero value when parameter is missing
			}

			if err := setFieldValue(field, fieldType.Type, []string{value}); err != nil {
				return fmt.Errorf("%w: field %s: %v", ErrFailedToParsePath, fieldType.Name, err)
			}
		}

		return nil
	}
}
