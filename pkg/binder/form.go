package binder

import (
	"fmt"
	"mime"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
)

// DefaultMaxMemory is the default maximum memory used for parsing multipart forms (10MB).
const DefaultMaxMemory = 10 << 20 // 10 MB

// Form creates a unified binder for both form data and file uploads.
// It handles application/x-www-form-urlencoded and multipart/form-data content types.
//
// Supported struct tags:
//   - `form:"name"` - binds to form field "name"
//   - `form:"-"`    - skips the field
//   - `file:"name"` - binds to uploaded file "name"
//   - `file:"-"`    - skips the field
//
// Supported types for form fields:
//   - Basic types: string, int, int64, uint, uint64, float32, float64, bool
//   - Slices of basic types for multi-value fields
//   - Pointers for optional fields
//
// Supported types for file fields:
//   - *multipart.FileHeader - single file
//   - []*multipart.FileHeader - multiple files
//
// Example:
//
//	type UploadRequest struct {
//		Title    string                  `form:"title"`
//		Category string                  `form:"category"`
//		Tags     []string                `form:"tags"`     // Multi-value field
//		Avatar   *multipart.FileHeader   `file:"avatar"`   // Optional file
//		Gallery  []*multipart.FileHeader `file:"gallery"`  // Multiple files
//		Internal string                  `form:"-"`        // Skipped
//	}
//
//	func uploadHandler(w http.ResponseWriter, r *http.Request) {
//		var req UploadRequest
//		if err := binder.Form()(r, &req); err != nil {
//			http.Error(w, err.Error(), http.StatusBadRequest)
//			return
//		}
//
//		if req.Avatar != nil {
//			file, err := req.Avatar.Open()
//			if err != nil {
//				http.Error(w, "Failed to open file", http.StatusInternalServerError)
//				return
//			}
//			defer file.Close()
//			// Process file...
//		}
//	}
//
//	http.HandleFunc("/upload", uploadHandler)
func Form() Binder {
	return func(r *http.Request, v any) error {
		contentType := r.Header.Get("Content-Type")
		if contentType == "" {
			return fmt.Errorf("%w: missing content-type header, expected application/x-www-form-urlencoded or multipart/form-data", ErrMissingContentType)
		}

		// Strip boundary and other parameters from Content-Type
		mediaType, _, _ := strings.Cut(contentType, ";")
		mediaType = strings.TrimSpace(mediaType)

		var values map[string][]string
		var files map[string][]*multipart.FileHeader

		switch {
		case mediaType == "application/x-www-form-urlencoded":
			if err := r.ParseForm(); err != nil {
				return fmt.Errorf("%w: %v", ErrFailedToParseForm, err)
			}
			values = r.Form

		case strings.HasPrefix(mediaType, "multipart/form-data"):
			// Parse and validate boundary parameter to prevent malformed multipart attacks
			_, params, err := mime.ParseMediaType(contentType)
			if err != nil {
				return fmt.Errorf("%w: malformed content type with boundary", ErrFailedToParseForm)
			}

			boundary, ok := params["boundary"]
			if !ok || boundary == "" {
				return fmt.Errorf("%w: missing boundary in content type", ErrFailedToParseForm)
			}

			if !validateBoundary(boundary) {
				return fmt.Errorf("%w: invalid boundary parameter", ErrFailedToParseForm)
			}

			// Use DefaultMaxMemory for multipart parsing; larger files spill to disk
			if err := r.ParseMultipartForm(DefaultMaxMemory); err != nil {
				return fmt.Errorf("%w: %v", ErrFailedToParseForm, err)
			}

			if r.MultipartForm != nil {
				values = r.MultipartForm.Value
				files = r.MultipartForm.File
			} else {
				values = make(map[string][]string)
			}

		default:
			return fmt.Errorf("%w: got %s, expected application/x-www-form-urlencoded or multipart/form-data", ErrUnsupportedMediaType, mediaType)
		}

		// Cleanup of multipart form is deferred to caller to allow access to files after binding
		return bindFormAndFiles(v, values, files, ErrFailedToParseForm)
	}
}

// bindFormAndFiles binds both form values and files to a struct.
func bindFormAndFiles(v any, values map[string][]string, files map[string][]*multipart.FileHeader, bindErr error) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return fmt.Errorf("%w: target must be a non-nil pointer", bindErr)
	}

	rv = rv.Elem()
	if rv.Kind() != reflect.Struct {
		return fmt.Errorf("%w: target must be a pointer to struct", bindErr)
	}

	rt := rv.Type()

	for i := range rv.NumField() {
		field := rv.Field(i)
		fieldType := rt.Field(i)

		// Skip unexported fields that reflection cannot modify
		if !field.CanSet() {
			continue
		}

		formTag := fieldType.Tag.Get("form")
		fileTag := fieldType.Tag.Get("file")

		// Skip if both tags are missing
		if formTag == "" && fileTag == "" {
			continue
		}

		// Handle form tag
		if formTag != "" {
			if formTag == "-" {
				continue // Skip explicitly ignored fields
			}

			// Parse tag to extract parameter name, ignoring additional options
			paramName, _, _ := strings.Cut(formTag, ",")

			// Skip malformed tags without parameter names
			if paramName == "" {
				continue
			}

			if fieldValues, exists := values[paramName]; exists && len(fieldValues) > 0 {
				if err := setFieldValue(field, fieldType.Type, fieldValues); err != nil {
					return fmt.Errorf("%w: field %s: %v", bindErr, fieldType.Name, err)
				}
			}
		}

		// Handle file uploads when multipart data is present
		if fileTag != "" && fileTag != "-" && files != nil {
			if fileHeaders, exists := files[fileTag]; exists && len(fileHeaders) > 0 {
				if err := setFileField(field, fieldType.Type, fileHeaders); err != nil {
					return fmt.Errorf("%w: field %s: %v", bindErr, fieldType.Name, err)
				}
			}
		}
	}

	return nil
}

// setFileField sets file values to struct fields.
func setFileField(field reflect.Value, fieldType reflect.Type, fileHeaders []*multipart.FileHeader) error {
	// Apply security sanitization to prevent path traversal attacks
	for _, fh := range fileHeaders {
		fh.Filename = sanitizeFilename(fh.Filename)
	}

	if fieldType.Kind() == reflect.Slice {
		elemType := fieldType.Elem()
		if elemType != reflect.TypeFor[*multipart.FileHeader]() {
			return fmt.Errorf("unsupported slice element type for file field: %v", elemType)
		}

		slice := reflect.MakeSlice(fieldType, len(fileHeaders), len(fileHeaders))
		for i, fh := range fileHeaders {
			slice.Index(i).Set(reflect.ValueOf(fh))
		}
		field.Set(slice)
		return nil
	}

	if fieldType == reflect.TypeFor[*multipart.FileHeader]() {
		if len(fileHeaders) > 0 {
			field.Set(reflect.ValueOf(fileHeaders[0]))
		}
		return nil
	}

	return fmt.Errorf("unsupported type for file field: %v (expected *multipart.FileHeader or []*multipart.FileHeader)", fieldType)
}

// sanitizeFilename removes path components and dangerous characters from uploaded filenames.
// This prevents path traversal attacks and ensures safe filename handling.
func sanitizeFilename(filename string) string {
	// Normalize path separators for consistent processing across platforms
	filename = strings.ReplaceAll(filename, "\\", "/")

	// Extract filename component only, discarding directory paths
	filename = filepath.Base(filename)

	// Remove null bytes and other potentially dangerous characters
	filename = strings.ReplaceAll(filename, "\x00", "")

	// Provide fallback name for empty or special directory references
	if filename == "." || filename == ".." || filename == "" || filename == "/" {
		filename = "unnamed"
	}

	return filename
}
