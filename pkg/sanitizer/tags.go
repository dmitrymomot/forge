package sanitizer

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
)

var (
	registryMu sync.RWMutex
	registry   = map[string]func(string) string{
		// String sanitizers
		"trim":        Trim,
		"lower":       ToLower,
		"upper":       ToUpper,
		"title":       ToTitle,
		"trim_lower":  TrimToLower,
		"trim_upper":  TrimToUpper,
		"kebab":       ToKebabCase,
		"snake":       ToSnakeCase,
		"camel":       ToCamelCase,
		"single_line": SingleLine,
		"no_spaces":   RemoveExtraWhitespace,
		"strip_html":  StripHTML,
		"alphanum":    KeepAlphanumeric,
		"alpha":       KeepAlpha,
		"digits":      KeepDigits,

		// Format sanitizers
		"email":       NormalizeEmail,
		"phone":       NormalizePhone,
		"url":         NormalizeURL,
		"domain":      ExtractDomain,
		"credit_card": NormalizeCreditCard,
		"ssn":         NormalizeSSN,
		"postal_code": NormalizePostalCode,
		"filename":    SanitizeFilename,
		"whitespace":  NormalizeWhitespace,

		// Security sanitizers
		"escape_html":     EscapeHTML,
		"unescape_html":   UnescapeHTML,
		"xss":             PreventXSS,
		"html":            SanitizeHTML, // Allow safe formatting (bluemonday)
		"sql_string":      EscapeSQLString,
		"sql_identifier":  SanitizeSQLIdentifier,
		"path":            SanitizePath,
		"path_traversal":  PreventPathTraversal,
		"shell_arg":       SanitizeShellArgument,
		"no_null":         RemoveNullBytes,
		"no_control":      RemoveControlChars,
		"user_input":      SanitizeUserInput,
		"secure_filename": SanitizeSecureFilename,
		"header":          PreventHeaderInjection,

		// Composite sanitizers for common use cases
		"username": func(s string) string {
			return KeepAlphanumeric(ToLower(Trim(s)))
		},
		"slug": func(s string) string {
			return ToKebabCase(Trim(s))
		},
		"name": func(s string) string {
			return ToTitle(RemoveExtraWhitespace(Trim(s)))
		},
		"text": func(s string) string {
			return RemoveExtraWhitespace(Trim(s))
		},
		"safe_text": func(s string) string {
			return EscapeHTML(RemoveExtraWhitespace(Trim(s)))
		},
		"safe_html": func(s string) string {
			return PreventXSS(Trim(s))
		},
	}
)

// RegisterSanitizer adds a custom sanitizer function to the registry
func RegisterSanitizer(name string, fn func(string) string) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[name] = fn
}

// SanitizeStruct applies sanitization to struct fields based on their tags
func SanitizeStruct(v any) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer {
		return errors.New("sanitizer: must pass a pointer to struct")
	}

	rv = rv.Elem()
	if rv.Kind() != reflect.Struct {
		return errors.New("sanitizer: must pass a pointer to struct")
	}

	return sanitizeStructRecursive(rv)
}

func sanitizeStructRecursive(rv reflect.Value) error {
	rt := rv.Type()

	for i := range rv.NumField() {
		field := rv.Field(i)
		if !field.CanSet() {
			continue
		}

		structField := rt.Field(i)
		tag := structField.Tag.Get("sanitize")

		// Skip if tag is "-"
		if tag == "-" {
			continue
		}

		// Handle different field types
		switch field.Kind() {
		case reflect.String:
			if tag == "" {
				continue // Skip strings without tags
			}
			value := field.String()
			sanitized, err := applySanitizers(value, tag)
			if err != nil {
				return err
			}
			field.SetString(sanitized)

		case reflect.Pointer:
			if !field.IsNil() {
				elem := field.Elem()
				if elem.Kind() == reflect.String {
					if tag != "" {
						value := elem.String()
						sanitized, err := applySanitizers(value, tag)
						if err != nil {
							return err
						}
						elem.SetString(sanitized)
					}
				} else if elem.Kind() == reflect.Struct {
					// Always recursively process nested structs
					if err := sanitizeStructRecursive(elem); err != nil {
						return err
					}
				}
			}

		case reflect.Struct:
			// Always recursively sanitize nested structs
			if err := sanitizeStructRecursive(field); err != nil {
				return err
			}

		case reflect.Slice:
			// Handle slices of strings only if tag is present
			if tag != "" && field.Type().Elem().Kind() == reflect.String {
				for j := 0; j < field.Len(); j++ {
					elem := field.Index(j)
					value := elem.String()
					sanitized, err := applySanitizers(value, tag)
					if err != nil {
						return err
					}
					elem.SetString(sanitized)
				}
			}
		}
	}

	return nil
}

func applySanitizers(value string, tag string) (string, error) {
	sanitizers := strings.Split(tag, ",")
	result := value

	registryMu.RLock()
	defer registryMu.RUnlock()

	for _, sanitizerName := range sanitizers {
		sanitizerName = strings.TrimSpace(sanitizerName)
		if sanitizerName == "" {
			continue
		}

		// Handle max length special case: max:100
		if strings.HasPrefix(sanitizerName, "max:") {
			parts := strings.Split(sanitizerName, ":")
			if len(parts) == 2 {
				// Parse the number after "max:"
				var maxLen int
				_, _ = fmt.Sscanf(parts[1], "%d", &maxLen)
				if maxLen > 0 {
					result = MaxLength(result, maxLen)
				}
			}
			continue
		}

		if fn, ok := registry[sanitizerName]; ok {
			result = fn(result)
		}
	}

	return result, nil
}
