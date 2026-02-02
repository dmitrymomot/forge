package binder

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// bindToStruct binds values to a struct using reflection.
// tagName specifies which struct tag to use (e.g., "query", "form").
// values is a map of parameter names to their string values.
// bindErr is the specific error to use for binding failures.
func bindToStruct(v any, tagName string, values map[string][]string, bindErr error) error {
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

		// Skip unexported fields
		if !field.CanSet() {
			continue
		}

		paramName, skip := parseFieldTag(fieldType, tagName)
		if skip {
			continue
		}

		fieldValues, exists := values[paramName]
		if !exists || len(fieldValues) == 0 {
			continue // No value provided, leave as zero value
		}

		if err := setFieldValue(field, fieldType.Type, fieldValues); err != nil {
			return fmt.Errorf("%w: field %s: %v", bindErr, fieldType.Name, err)
		}
	}

	return nil
}

// parseFieldTag extracts the parameter name from struct tags and determines if the field should be skipped.
// If no tag is present, it defaults to the lowercase field name.
func parseFieldTag(field reflect.StructField, tagName string) (paramName string, skip bool) {
	tag := field.Tag.Get(tagName)
	if tag == "" {
		return strings.ToLower(field.Name), false
	}
	if tag == "-" {
		return "", true
	}

	// Extract parameter name from comma-separated tag options
	tagParts := strings.Split(tag, ",")
	if len(tagParts) == 0 {
		return "", false
	}
	return tagParts[0], false
}

// setFieldValue sets the field value from string values.
func setFieldValue(field reflect.Value, fieldType reflect.Type, values []string) error {
	// Dereference pointers, creating new instances for nil pointers
	if fieldType.Kind() == reflect.Pointer {
		if field.IsNil() {
			field.Set(reflect.New(fieldType.Elem()))
		}
		return setFieldValue(field.Elem(), fieldType.Elem(), values)
	}

	// Process slice types with multiple values or comma-separated values
	if fieldType.Kind() == reflect.Slice {
		return setSliceValue(field, fieldType, values)
	}

	// Use first value for scalar types, ignoring additional values
	if len(values) == 0 {
		return nil
	}
	value := values[0]

	switch fieldType.Kind() {
	case reflect.String:
		field.SetString(sanitizeStringValue(value))

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(value, 10, fieldType.Bits())
		if err != nil {
			return fmt.Errorf("invalid int value %q", value)
		}
		field.SetInt(n)

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(value, 10, fieldType.Bits())
		if err != nil {
			return fmt.Errorf("invalid uint value %q", value)
		}
		field.SetUint(n)

	case reflect.Float32, reflect.Float64:
		n, err := strconv.ParseFloat(value, fieldType.Bits())
		if err != nil {
			return fmt.Errorf("invalid float value %q", value)
		}
		field.SetFloat(n)

	case reflect.Bool:
		b, err := strconv.ParseBool(value)
		if err != nil {
			// Accept common boolean representations for user-friendly parsing
			switch strings.ToLower(value) {
			case "on", "yes", "1":
				b = true
			case "off", "no", "0", "":
				b = false
			default:
				return fmt.Errorf("invalid bool value %q", value)
			}
		}
		field.SetBool(b)

	default:
		return fmt.Errorf("unsupported type %s", fieldType.Kind())
	}

	return nil
}

// setSliceValue sets slice field values from string values.
func setSliceValue(field reflect.Value, fieldType reflect.Type, values []string) error {
	elemType := fieldType.Elem()

	// Handle both multiple form fields and comma-separated values in single field
	var allValues []string
	for _, v := range values {
		if strings.Contains(v, ",") {
			allValues = append(allValues, strings.Split(v, ",")...)
		} else {
			allValues = append(allValues, v)
		}
	}

	slice := reflect.MakeSlice(fieldType, len(allValues), len(allValues))

	for i, value := range allValues {
		elem := slice.Index(i)
		if err := setFieldValue(elem, elemType, []string{strings.TrimSpace(value)}); err != nil {
			return err
		}
	}

	field.Set(slice)
	return nil
}

// sanitizeStringValue removes dangerous characters that could be used in injection attacks.
// It prevents CRLF injection, null byte attacks, and filters invalid Unicode sequences.
func sanitizeStringValue(value string) string {
	// Remove NUL bytes
	value = strings.ReplaceAll(value, "\x00", "")

	// Strip carriage return and line feed to prevent HTTP header injection
	value = strings.ReplaceAll(value, "\r\n", "")
	value = strings.ReplaceAll(value, "\r", "")
	value = strings.ReplaceAll(value, "\n", "")

	// Filter out control characters while preserving printable content
	var builder strings.Builder
	builder.Grow(len(value))

	for _, r := range value {
		if r == '\t' || r >= ' ' || unicode.IsGraphic(r) {
			if utf8.ValidRune(r) {
				builder.WriteRune(r)
			}
		}
	}

	return builder.String()
}

// validateBoundary performs security validation on multipart form boundaries.
// It prevents parsing attacks by rejecting malformed or dangerous boundary values.
func validateBoundary(boundary string) bool {
	if boundary == "" {
		return false
	}

	// Reject boundaries containing characters that break multipart parsing
	for _, r := range boundary {
		if r == '\x00' || r == '\r' || r == '\n' {
			return false
		}
	}

	// Enforce reasonable length limit to prevent DoS attacks
	if len(boundary) > 100 {
		return false
	}

	return true
}
