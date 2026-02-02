package binder

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
)

// DefaultMaxJSONSize is the default maximum size for JSON request bodies (1MB).
const DefaultMaxJSONSize = 1 << 20 // 1 MB

// JSON creates a JSON binder function.
//
// Example:
//
//	func createUserHandler(w http.ResponseWriter, r *http.Request) {
//		var req CreateUserRequest
//		if err := binder.JSON()(r, &req); err != nil {
//			http.Error(w, err.Error(), http.StatusBadRequest)
//			return
//		}
//		// req is populated from JSON body
//		// Process req and return response...
//	}
//
//	http.HandleFunc("/users", createUserHandler)
func JSON() Binder {
	return func(r *http.Request, v any) error {
		// Fail fast if request context is already cancelled to avoid processing doomed requests
		ctx := r.Context()
		if ctx != nil {
			select {
			case <-ctx.Done():
				return fmt.Errorf("%w: context timeout", ErrFailedToParseJSON)
			default:
			}
		}

		contentType := r.Header.Get("Content-Type")
		if contentType == "" {
			return fmt.Errorf("%w: missing content-type header, expected application/json", ErrMissingContentType)
		}

		// Strip charset and other parameters from Content-Type (e.g., "application/json; charset=utf-8")
		mediaType, _, _ := strings.Cut(contentType, ";")
		mediaType = strings.TrimSpace(mediaType)

		if mediaType != "application/json" {
			return fmt.Errorf("%w: got %s, expected application/json", ErrUnsupportedMediaType, mediaType)
		}

		// Read entire body with +1 byte to detect oversized requests efficiently
		limitedReader := io.LimitReader(r.Body, DefaultMaxJSONSize+1)
		body, err := io.ReadAll(limitedReader)
		if err != nil {
			return fmt.Errorf("%w: failed to read request body: %v", ErrFailedToParseJSON, err)
		}

		// Reject requests exceeding size limit to prevent DoS attacks
		if len(body) > DefaultMaxJSONSize {
			return fmt.Errorf("%w: request body too large (max %d bytes)", ErrFailedToParseJSON, DefaultMaxJSONSize)
		}

		decoder := json.NewDecoder(strings.NewReader(string(body)))
		decoder.DisallowUnknownFields() // Strict mode prevents typos and unexpected fields

		if err := decoder.Decode(v); err != nil {
			switch {
			case strings.Contains(err.Error(), "cannot unmarshal"):
				return fmt.Errorf("%w: %v", ErrFailedToParseJSON, err)
			case strings.Contains(err.Error(), "unexpected end of JSON"):
				return fmt.Errorf("%w: %v", ErrFailedToParseJSON, err)
			case strings.Contains(err.Error(), "invalid character"):
				return fmt.Errorf("%w: %v", ErrFailedToParseJSON, err)
			case err == io.EOF:
				return fmt.Errorf("%w: empty body", ErrFailedToParseJSON)
			default:
				return fmt.Errorf("%w: %v", ErrFailedToParseJSON, err)
			}
		}

		// Verify no trailing data exists after valid JSON to prevent injection attacks
		var extra json.RawMessage
		if err := decoder.Decode(&extra); err != io.EOF {
			return fmt.Errorf("%w: unexpected data after JSON object", ErrFailedToParseJSON)
		}

		// Apply security sanitization to prevent XSS and injection attacks
		if err := sanitizeJSONStruct(v); err != nil {
			return fmt.Errorf("%w: failed to sanitize input: %v", ErrFailedToParseJSON, err)
		}

		return nil
	}
}

// sanitizeJSONStruct recursively sanitizes all string fields in a struct.
func sanitizeJSONStruct(v any) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return nil
	}

	rv = rv.Elem()
	return sanitizeReflectValue(rv)
}

// sanitizeReflectValue recursively sanitizes reflect.Value.
func sanitizeReflectValue(rv reflect.Value) error {
	switch rv.Kind() {
	case reflect.String:
		if rv.CanSet() {
			sanitized := sanitizeStringValue(rv.String())
			rv.SetString(sanitized)
		}

	case reflect.Struct:
		for i := range rv.NumField() {
			field := rv.Field(i)
			if field.CanSet() {
				if err := sanitizeReflectValue(field); err != nil {
					return err
				}
			}
		}

	case reflect.Slice, reflect.Array:
		for i := range rv.Len() {
			elem := rv.Index(i)
			if err := sanitizeReflectValue(elem); err != nil {
				return err
			}
		}

	case reflect.Map:
		for _, key := range rv.MapKeys() {
			value := rv.MapIndex(key)
			if value.CanSet() {
				if err := sanitizeReflectValue(value); err != nil {
					return err
				}
			}
		}

	case reflect.Pointer:
		if !rv.IsNil() {
			if err := sanitizeReflectValue(rv.Elem()); err != nil {
				return err
			}
		}

	case reflect.Interface:
		if !rv.IsNil() {
			if err := sanitizeReflectValue(rv.Elem()); err != nil {
				return err
			}
		}
	}

	return nil
}
