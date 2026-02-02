package validator

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ValidatorFunc is a function that validates a value and returns a Rule
type ValidatorFunc func(field string, value reflect.Value, params []string) Rule

var (
	registryMu sync.RWMutex
	registry   = map[string]ValidatorFunc{
		// String validators
		"required": requiredValidator,
		"min":      minValidator,
		"max":      maxValidator,
		"len":      lenValidator,
		"between":  betweenValidator,
		"email":    emailValidator,
		"url":      urlValidator,
		"phone":    phoneValidator,
		"alphanum": alphanumValidator,
		"alpha":    alphaValidator,
		"numeric":  numericValidator,
		"uuid":     uuidValidator,
		"in":       inValidator,
		"not_in":   notInValidator,
		"contains": containsValidator,
		"prefix":   prefixValidator,
		"suffix":   suffixValidator,
		"regex":    regexValidator,

		// Date validators
		"date":        dateValidator,
		"date_format": dateFormatValidator,
		"after":       afterValidator,
		"before":      beforeValidator,

		// Numeric validators
		"positive": positiveValidator,
		"negative": negativeValidator,
		"zero":     zeroValidator,
		"nonzero":  nonZeroValidator,

		// Network/Format validators
		"ipv4": ipv4Validator,
		"ipv6": ipv6Validator,
		"ip":   ipValidator,
		"mac":  macValidator,

		// Identifier validators
		"slug":          slugValidator,
		"username":      usernameValidator,
		"handle":        handleValidator,
		"sku":           skuValidator,
		"hex":           hexValidator,
		"base64":        base64Validator,
		"domain":        domainValidator,
		"subdomain":     subdomainValidator,
		"api_key":       apiKeyValidator,
		"ticket_number": ticketNumberValidator,
		"version":       versionValidator,
		"semver":        versionValidator, // alias for version

		// Financial validators
		"currency":   currencyValidator,
		"percentage": percentageValidator,

		// Password validators
		"strong_password":     strongPasswordValidator,
		"password_uppercase":  passwordUppercaseValidator,
		"password_lowercase":  passwordLowercaseValidator,
		"password_digit":      passwordDigitValidator,
		"password_special":    passwordSpecialValidator,
		"not_common_password": notCommonPasswordValidator,

		// Pattern/Character validators
		"ascii":              asciiValidator,
		"no_whitespace":      noWhitespaceValidator,
		"no_control_chars":   noControlCharsValidator,
		"printable":          printableValidator,
		"no_special_chars":   noSpecialCharsValidator,
		"contains_uppercase": containsUppercaseValidator,
		"contains_lowercase": containsLowercaseValidator,
		"contains_digit":     containsDigitValidator,
		"otp":                otpValidator,
	}
)

// RegisterValidator adds a custom validator function to the registry
func RegisterValidator(name string, fn ValidatorFunc) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[name] = fn
}

// ValidateStruct validates a struct based on its field tags
func ValidateStruct(v any) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer {
		return fmt.Errorf("validator: must pass a pointer to struct")
	}

	rv = rv.Elem()
	if rv.Kind() != reflect.Struct {
		return fmt.Errorf("validator: must pass a pointer to struct")
	}

	var errors ValidationErrors
	validateStructRecursive(rv, "", &errors)

	if errors.IsEmpty() {
		return nil
	}
	return errors
}

func validateStructRecursive(rv reflect.Value, prefix string, errors *ValidationErrors) {
	rt := rv.Type()

	for i := range rv.NumField() {
		field := rv.Field(i)
		if !field.CanSet() {
			continue
		}

		structField := rt.Field(i)
		tag := structField.Tag.Get("validate")

		// Build field path
		fieldPath := structField.Name
		if prefix != "" {
			fieldPath = prefix + "." + structField.Name
		}

		// Skip if tag is "-"
		if tag == "-" {
			continue
		}

		// Handle nested structs (always process them)
		if field.Kind() == reflect.Struct && tag == "" {
			validateStructRecursive(field, fieldPath, errors)
			continue
		}

		// Handle pointers
		if field.Kind() == reflect.Pointer {
			if field.IsNil() {
				// If nil and has validation tag, might need to validate required
				if tag != "" {
					validateField(fieldPath, field, tag, errors)
				}
			} else {
				elem := field.Elem()
				if elem.Kind() == reflect.Struct && tag == "" {
					validateStructRecursive(elem, fieldPath, errors)
				} else if tag != "" {
					validateField(fieldPath, elem, tag, errors)
				}
			}
			continue
		}

		// Skip if no tag
		if tag == "" {
			continue
		}

		// Validate the field
		validateField(fieldPath, field, tag, errors)
	}
}

func validateField(fieldPath string, field reflect.Value, tag string, errors *ValidationErrors) {
	// Parse validation rules separated by semicolon
	rules := strings.Split(tag, ";")

	registryMu.RLock()
	defer registryMu.RUnlock()

	for _, ruleStr := range rules {
		ruleStr = strings.TrimSpace(ruleStr)
		if ruleStr == "" {
			continue
		}

		// Split rule name and parameters
		parts := strings.SplitN(ruleStr, ":", 2)
		if len(parts) == 0 {
			continue
		}
		ruleName := strings.TrimSpace(parts[0])

		var params []string
		if len(parts) > 1 {
			// Split parameters by comma
			paramStr := strings.TrimSpace(parts[1])
			if paramStr != "" {
				params = strings.Split(paramStr, ",")
				for i := range params {
					params[i] = strings.TrimSpace(params[i])
				}
			}
		}

		// Get validator function
		if validatorFn, ok := registry[ruleName]; ok {
			rule := validatorFn(fieldPath, field, params)
			if !rule.Check() {
				errors.Add(rule.Error)
			}
		}
	}
}

// Built-in validators

func requiredValidator(field string, value reflect.Value, params []string) Rule {
	return Rule{
		Check: func() bool {
			switch value.Kind() {
			case reflect.String:
				return strings.TrimSpace(value.String()) != ""
			case reflect.Slice, reflect.Map, reflect.Array:
				return value.Len() > 0
			case reflect.Pointer, reflect.Interface:
				return !value.IsNil()
			default:
				// For numbers, consider zero values as empty
				return !value.IsZero()
			}
		},
		Error: ValidationError{
			Field:          field,
			Message:        "field is required",
			TranslationKey: "validation.required",
			TranslationValues: map[string]any{
				"field": field,
			},
		},
	}
}

func minValidator(field string, value reflect.Value, params []string) Rule {
	if len(params) < 1 {
		return Rule{Check: func() bool { return true }}
	}

	switch value.Kind() {
	case reflect.String:
		min, _ := strconv.Atoi(params[0])
		return MinLenString(field, value.String(), min)
	case reflect.Slice, reflect.Array:
		min, _ := strconv.Atoi(params[0])
		return Rule{
			Check: func() bool {
				return value.Len() >= min
			},
			Error: ValidationError{
				Field:          field,
				Message:        fmt.Sprintf("must have at least %d items", min),
				TranslationKey: "validation.min_items",
				TranslationValues: map[string]any{
					"field": field,
					"min":   min,
				},
			},
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		min, _ := strconv.ParseInt(params[0], 10, 64)
		return Rule{
			Check: func() bool {
				return value.Int() >= min
			},
			Error: ValidationError{
				Field:          field,
				Message:        fmt.Sprintf("must be at least %d", min),
				TranslationKey: "validation.min",
				TranslationValues: map[string]any{
					"field": field,
					"min":   min,
				},
			},
		}
	case reflect.Float32, reflect.Float64:
		min, _ := strconv.ParseFloat(params[0], 64)
		return Rule{
			Check: func() bool {
				return value.Float() >= min
			},
			Error: ValidationError{
				Field:          field,
				Message:        fmt.Sprintf("must be at least %f", min),
				TranslationKey: "validation.min",
				TranslationValues: map[string]any{
					"field": field,
					"min":   min,
				},
			},
		}
	default:
		return Rule{Check: func() bool { return true }}
	}
}

func maxValidator(field string, value reflect.Value, params []string) Rule {
	if len(params) < 1 {
		return Rule{Check: func() bool { return true }}
	}

	switch value.Kind() {
	case reflect.String:
		max, _ := strconv.Atoi(params[0])
		return MaxLenString(field, value.String(), max)
	case reflect.Slice, reflect.Array:
		max, _ := strconv.Atoi(params[0])
		return Rule{
			Check: func() bool {
				return value.Len() <= max
			},
			Error: ValidationError{
				Field:          field,
				Message:        fmt.Sprintf("must have at most %d items", max),
				TranslationKey: "validation.max_items",
				TranslationValues: map[string]any{
					"field": field,
					"max":   max,
				},
			},
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		max, _ := strconv.ParseInt(params[0], 10, 64)
		return Rule{
			Check: func() bool {
				return value.Int() <= max
			},
			Error: ValidationError{
				Field:          field,
				Message:        fmt.Sprintf("must be at most %d", max),
				TranslationKey: "validation.max",
				TranslationValues: map[string]any{
					"field": field,
					"max":   max,
				},
			},
		}
	case reflect.Float32, reflect.Float64:
		max, _ := strconv.ParseFloat(params[0], 64)
		return Rule{
			Check: func() bool {
				return value.Float() <= max
			},
			Error: ValidationError{
				Field:          field,
				Message:        fmt.Sprintf("must be at most %f", max),
				TranslationKey: "validation.max",
				TranslationValues: map[string]any{
					"field": field,
					"max":   max,
				},
			},
		}
	default:
		return Rule{Check: func() bool { return true }}
	}
}

func lenValidator(field string, value reflect.Value, params []string) Rule {
	if len(params) < 1 {
		return Rule{Check: func() bool { return true }}
	}

	expectedLen, _ := strconv.Atoi(params[0])

	switch value.Kind() {
	case reflect.String:
		return Rule{
			Check: func() bool {
				return len(value.String()) == expectedLen
			},
			Error: ValidationError{
				Field:          field,
				Message:        fmt.Sprintf("must be exactly %d characters long", expectedLen),
				TranslationKey: "validation.exact_length",
				TranslationValues: map[string]any{
					"field": field,
					"len":   expectedLen,
				},
			},
		}
	case reflect.Slice, reflect.Array:
		return Rule{
			Check: func() bool {
				return value.Len() == expectedLen
			},
			Error: ValidationError{
				Field:          field,
				Message:        fmt.Sprintf("must have exactly %d items", expectedLen),
				TranslationKey: "validation.exact_items",
				TranslationValues: map[string]any{
					"field": field,
					"len":   expectedLen,
				},
			},
		}
	default:
		return Rule{Check: func() bool { return true }}
	}
}

func betweenValidator(field string, value reflect.Value, params []string) Rule {
	if len(params) < 2 {
		return Rule{Check: func() bool { return true }}
	}

	switch value.Kind() {
	case reflect.String:
		min, _ := strconv.Atoi(params[0])
		max, _ := strconv.Atoi(params[1])
		return Rule{
			Check: func() bool {
				l := len(value.String())
				return l >= min && l <= max
			},
			Error: ValidationError{
				Field:          field,
				Message:        fmt.Sprintf("must be between %d and %d characters long", min, max),
				TranslationKey: "validation.between_length",
				TranslationValues: map[string]any{
					"field": field,
					"min":   min,
					"max":   max,
				},
			},
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		min, _ := strconv.ParseInt(params[0], 10, 64)
		max, _ := strconv.ParseInt(params[1], 10, 64)
		return Rule{
			Check: func() bool {
				v := value.Int()
				return v >= min && v <= max
			},
			Error: ValidationError{
				Field:          field,
				Message:        fmt.Sprintf("must be between %d and %d", min, max),
				TranslationKey: "validation.between",
				TranslationValues: map[string]any{
					"field": field,
					"min":   min,
					"max":   max,
				},
			},
		}
	case reflect.Float32, reflect.Float64:
		min, _ := strconv.ParseFloat(params[0], 64)
		max, _ := strconv.ParseFloat(params[1], 64)
		return Rule{
			Check: func() bool {
				v := value.Float()
				return v >= min && v <= max
			},
			Error: ValidationError{
				Field:          field,
				Message:        fmt.Sprintf("must be between %f and %f", min, max),
				TranslationKey: "validation.between",
				TranslationValues: map[string]any{
					"field": field,
					"min":   min,
					"max":   max,
				},
			},
		}
	default:
		return Rule{Check: func() bool { return true }}
	}
}

func emailValidator(field string, value reflect.Value, params []string) Rule {
	if value.Kind() != reflect.String {
		return Rule{Check: func() bool { return true }}
	}
	return ValidEmail(field, value.String())
}

func urlValidator(field string, value reflect.Value, params []string) Rule {
	if value.Kind() != reflect.String {
		return Rule{Check: func() bool { return true }}
	}
	return ValidURL(field, value.String())
}

func phoneValidator(field string, value reflect.Value, params []string) Rule {
	if value.Kind() != reflect.String {
		return Rule{Check: func() bool { return true }}
	}
	return ValidPhone(field, value.String())
}

func alphanumValidator(field string, value reflect.Value, params []string) Rule {
	if value.Kind() != reflect.String {
		return Rule{Check: func() bool { return true }}
	}
	return ValidAlphanumeric(field, value.String())
}

func alphaValidator(field string, value reflect.Value, params []string) Rule {
	if value.Kind() != reflect.String {
		return Rule{Check: func() bool { return true }}
	}
	return ValidAlpha(field, value.String())
}

func numericValidator(field string, value reflect.Value, params []string) Rule {
	if value.Kind() != reflect.String {
		return Rule{Check: func() bool { return true }}
	}
	return ValidNumericString(field, value.String())
}

func uuidValidator(field string, value reflect.Value, params []string) Rule {
	if value.Kind() != reflect.String {
		return Rule{Check: func() bool { return true }}
	}

	version := 0 // Any version
	if len(params) > 0 {
		version, _ = strconv.Atoi(params[0])
	}

	if version > 0 {
		return ValidUUIDVersionString(field, value.String(), version)
	}
	return ValidUUID(field, value.String())
}

func inValidator(field string, value reflect.Value, params []string) Rule {
	if value.Kind() != reflect.String {
		return Rule{Check: func() bool { return true }}
	}
	return InList(field, value.String(), params)
}

func notInValidator(field string, value reflect.Value, params []string) Rule {
	if value.Kind() != reflect.String {
		return Rule{Check: func() bool { return true }}
	}
	return NotInList(field, value.String(), params)
}

func containsValidator(field string, value reflect.Value, params []string) Rule {
	if value.Kind() != reflect.String || len(params) < 1 {
		return Rule{Check: func() bool { return true }}
	}
	substring := params[0]
	return Rule{
		Check: func() bool {
			return strings.Contains(value.String(), substring)
		},
		Error: ValidationError{
			Field:          field,
			Message:        fmt.Sprintf("must contain '%s'", substring),
			TranslationKey: "validation.contains",
			TranslationValues: map[string]any{
				"field":     field,
				"substring": substring,
			},
		},
	}
}

func prefixValidator(field string, value reflect.Value, params []string) Rule {
	if value.Kind() != reflect.String || len(params) < 1 {
		return Rule{Check: func() bool { return true }}
	}
	prefix := params[0]
	return Rule{
		Check: func() bool {
			return strings.HasPrefix(value.String(), prefix)
		},
		Error: ValidationError{
			Field:          field,
			Message:        fmt.Sprintf("must start with '%s'", prefix),
			TranslationKey: "validation.prefix",
			TranslationValues: map[string]any{
				"field":  field,
				"prefix": prefix,
			},
		},
	}
}

func suffixValidator(field string, value reflect.Value, params []string) Rule {
	if value.Kind() != reflect.String || len(params) < 1 {
		return Rule{Check: func() bool { return true }}
	}
	suffix := params[0]
	return Rule{
		Check: func() bool {
			return strings.HasSuffix(value.String(), suffix)
		},
		Error: ValidationError{
			Field:          field,
			Message:        fmt.Sprintf("must end with '%s'", suffix),
			TranslationKey: "validation.suffix",
			TranslationValues: map[string]any{
				"field":  field,
				"suffix": suffix,
			},
		},
	}
}

func regexValidator(field string, value reflect.Value, params []string) Rule {
	if value.Kind() != reflect.String || len(params) < 1 {
		return Rule{Check: func() bool { return true }}
	}
	pattern := params[0]
	description := "pattern"
	if len(params) > 1 {
		description = params[1]
	}
	return MatchesRegex(field, value.String(), pattern, description)
}

func dateValidator(field string, value reflect.Value, params []string) Rule {
	if value.Kind() != reflect.String {
		return Rule{Check: func() bool { return true }}
	}

	// Try common date formats
	formats := []string{
		"2006-01-02",
		"2006-01-02 15:04:05",
		time.RFC3339,
	}

	return Rule{
		Check: func() bool {
			for _, format := range formats {
				if _, err := time.Parse(format, value.String()); err == nil {
					return true
				}
			}
			return false
		},
		Error: ValidationError{
			Field:          field,
			Message:        "must be a valid date",
			TranslationKey: "validation.date",
			TranslationValues: map[string]any{
				"field": field,
			},
		},
	}
}

func dateFormatValidator(field string, value reflect.Value, params []string) Rule {
	if value.Kind() != reflect.String || len(params) < 1 {
		return Rule{Check: func() bool { return true }}
	}

	format := params[0]
	return Rule{
		Check: func() bool {
			_, err := time.Parse(format, value.String())
			return err == nil
		},
		Error: ValidationError{
			Field:          field,
			Message:        fmt.Sprintf("must be a valid date in format %s", format),
			TranslationKey: "validation.date_format",
			TranslationValues: map[string]any{
				"field":  field,
				"format": format,
			},
		},
	}
}

func afterValidator(field string, value reflect.Value, params []string) Rule {
	if value.Kind() != reflect.String || len(params) < 1 {
		return Rule{Check: func() bool { return true }}
	}

	afterStr := params[0]
	return Rule{
		Check: func() bool {
			t, err := time.Parse(time.RFC3339, value.String())
			if err != nil {
				return false
			}
			after, err := time.Parse(time.RFC3339, afterStr)
			if err != nil {
				// Try to parse as date only
				after, err = time.Parse("2006-01-02", afterStr)
				if err != nil {
					return false
				}
			}
			return t.After(after)
		},
		Error: ValidationError{
			Field:          field,
			Message:        fmt.Sprintf("must be after %s", afterStr),
			TranslationKey: "validation.after",
			TranslationValues: map[string]any{
				"field": field,
				"after": afterStr,
			},
		},
	}
}

func beforeValidator(field string, value reflect.Value, params []string) Rule {
	if value.Kind() != reflect.String || len(params) < 1 {
		return Rule{Check: func() bool { return true }}
	}

	beforeStr := params[0]
	return Rule{
		Check: func() bool {
			t, err := time.Parse(time.RFC3339, value.String())
			if err != nil {
				return false
			}
			before, err := time.Parse(time.RFC3339, beforeStr)
			if err != nil {
				// Try to parse as date only
				before, err = time.Parse("2006-01-02", beforeStr)
				if err != nil {
					return false
				}
			}
			return t.Before(before)
		},
		Error: ValidationError{
			Field:          field,
			Message:        fmt.Sprintf("must be before %s", beforeStr),
			TranslationKey: "validation.before",
			TranslationValues: map[string]any{
				"field":  field,
				"before": beforeStr,
			},
		},
	}
}

func positiveValidator(field string, value reflect.Value, params []string) Rule {
	switch value.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return Rule{
			Check: func() bool {
				return value.Int() > 0
			},
			Error: ValidationError{
				Field:          field,
				Message:        "must be positive",
				TranslationKey: "validation.positive",
				TranslationValues: map[string]any{
					"field": field,
				},
			},
		}
	case reflect.Float32, reflect.Float64:
		return Rule{
			Check: func() bool {
				return value.Float() > 0
			},
			Error: ValidationError{
				Field:          field,
				Message:        "must be positive",
				TranslationKey: "validation.positive",
				TranslationValues: map[string]any{
					"field": field,
				},
			},
		}
	default:
		return Rule{Check: func() bool { return true }}
	}
}

func negativeValidator(field string, value reflect.Value, params []string) Rule {
	switch value.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return Rule{
			Check: func() bool {
				return value.Int() < 0
			},
			Error: ValidationError{
				Field:          field,
				Message:        "must be negative",
				TranslationKey: "validation.negative",
				TranslationValues: map[string]any{
					"field": field,
				},
			},
		}
	case reflect.Float32, reflect.Float64:
		return Rule{
			Check: func() bool {
				return value.Float() < 0
			},
			Error: ValidationError{
				Field:          field,
				Message:        "must be negative",
				TranslationKey: "validation.negative",
				TranslationValues: map[string]any{
					"field": field,
				},
			},
		}
	default:
		return Rule{Check: func() bool { return true }}
	}
}

func zeroValidator(field string, value reflect.Value, params []string) Rule {
	return Rule{
		Check: func() bool {
			return value.IsZero()
		},
		Error: ValidationError{
			Field:          field,
			Message:        "must be zero",
			TranslationKey: "validation.zero",
			TranslationValues: map[string]any{
				"field": field,
			},
		},
	}
}

func nonZeroValidator(field string, value reflect.Value, params []string) Rule {
	return Rule{
		Check: func() bool {
			return !value.IsZero()
		},
		Error: ValidationError{
			Field:          field,
			Message:        "must not be zero",
			TranslationKey: "validation.nonzero",
			TranslationValues: map[string]any{
				"field": field,
			},
		},
	}
}

// Network/Format validators

func ipv4Validator(field string, value reflect.Value, params []string) Rule {
	if value.Kind() != reflect.String {
		return Rule{Check: func() bool { return true }}
	}
	return ValidIPv4(field, value.String())
}

func ipv6Validator(field string, value reflect.Value, params []string) Rule {
	if value.Kind() != reflect.String {
		return Rule{Check: func() bool { return true }}
	}
	return ValidIPv6(field, value.String())
}

func ipValidator(field string, value reflect.Value, params []string) Rule {
	if value.Kind() != reflect.String {
		return Rule{Check: func() bool { return true }}
	}
	return ValidIP(field, value.String())
}

func macValidator(field string, value reflect.Value, params []string) Rule {
	if value.Kind() != reflect.String {
		return Rule{Check: func() bool { return true }}
	}
	return ValidMAC(field, value.String())
}

// Identifier validators

func slugValidator(field string, value reflect.Value, params []string) Rule {
	if value.Kind() != reflect.String {
		return Rule{Check: func() bool { return true }}
	}
	return ValidSlug(field, value.String())
}

func usernameValidator(field string, value reflect.Value, params []string) Rule {
	if value.Kind() != reflect.String {
		return Rule{Check: func() bool { return true }}
	}
	minLen := 3
	maxLen := 20
	if len(params) >= 2 {
		if min, err := strconv.Atoi(params[0]); err == nil {
			minLen = min
		}
		if max, err := strconv.Atoi(params[1]); err == nil {
			maxLen = max
		}
	}
	return ValidUsername(field, value.String(), minLen, maxLen)
}

func handleValidator(field string, value reflect.Value, params []string) Rule {
	if value.Kind() != reflect.String {
		return Rule{Check: func() bool { return true }}
	}
	minLen := 3
	maxLen := 20
	if len(params) >= 2 {
		if min, err := strconv.Atoi(params[0]); err == nil {
			minLen = min
		}
		if max, err := strconv.Atoi(params[1]); err == nil {
			maxLen = max
		}
	}
	return ValidHandle(field, value.String(), minLen, maxLen)
}

func skuValidator(field string, value reflect.Value, params []string) Rule {
	if value.Kind() != reflect.String {
		return Rule{Check: func() bool { return true }}
	}
	return ValidSKU(field, value.String())
}

func hexValidator(field string, value reflect.Value, params []string) Rule {
	if value.Kind() != reflect.String {
		return Rule{Check: func() bool { return true }}
	}
	exactLength := 0
	if len(params) >= 1 {
		if length, err := strconv.Atoi(params[0]); err == nil {
			exactLength = length
		}
	}
	return ValidHexString(field, value.String(), exactLength)
}

func base64Validator(field string, value reflect.Value, params []string) Rule {
	if value.Kind() != reflect.String {
		return Rule{Check: func() bool { return true }}
	}
	return ValidBase64(field, value.String())
}

func domainValidator(field string, value reflect.Value, params []string) Rule {
	if value.Kind() != reflect.String {
		return Rule{Check: func() bool { return true }}
	}
	return ValidDomainName(field, value.String())
}

func subdomainValidator(field string, value reflect.Value, params []string) Rule {
	if value.Kind() != reflect.String {
		return Rule{Check: func() bool { return true }}
	}
	return ValidSubdomain(field, value.String())
}

func apiKeyValidator(field string, value reflect.Value, params []string) Rule {
	if value.Kind() != reflect.String {
		return Rule{Check: func() bool { return true }}
	}
	minLength := 32
	maxLength := 64
	if len(params) >= 2 {
		if min, err := strconv.Atoi(params[0]); err == nil {
			minLength = min
		}
		if max, err := strconv.Atoi(params[1]); err == nil {
			maxLength = max
		}
	}
	return ValidAPIKey(field, value.String(), minLength, maxLength)
}

func ticketNumberValidator(field string, value reflect.Value, params []string) Rule {
	if value.Kind() != reflect.String {
		return Rule{Check: func() bool { return true }}
	}
	prefix := ""
	if len(params) >= 1 {
		prefix = params[0]
	}
	return ValidTicketNumber(field, value.String(), prefix)
}

func versionValidator(field string, value reflect.Value, params []string) Rule {
	if value.Kind() != reflect.String {
		return Rule{Check: func() bool { return true }}
	}
	return ValidVersion(field, value.String())
}

// Financial validators

func currencyValidator(field string, value reflect.Value, params []string) Rule {
	if value.Kind() != reflect.String {
		return Rule{Check: func() bool { return true }}
	}
	return ValidCurrencyCode(field, value.String())
}

func percentageValidator(field string, value reflect.Value, params []string) Rule {
	switch value.Kind() {
	case reflect.Float32, reflect.Float64:
		return ValidPercentage(field, value.Float())
	default:
		return Rule{Check: func() bool { return true }}
	}
}

// Password validators

func strongPasswordValidator(field string, value reflect.Value, params []string) Rule {
	if value.Kind() != reflect.String {
		return Rule{Check: func() bool { return true }}
	}
	return StrongPassword(field, value.String(), DefaultPasswordStrength())
}

func passwordUppercaseValidator(field string, value reflect.Value, params []string) Rule {
	if value.Kind() != reflect.String {
		return Rule{Check: func() bool { return true }}
	}
	return PasswordUppercase(field, value.String())
}

func passwordLowercaseValidator(field string, value reflect.Value, params []string) Rule {
	if value.Kind() != reflect.String {
		return Rule{Check: func() bool { return true }}
	}
	return PasswordLowercase(field, value.String())
}

func passwordDigitValidator(field string, value reflect.Value, params []string) Rule {
	if value.Kind() != reflect.String {
		return Rule{Check: func() bool { return true }}
	}
	return PasswordDigit(field, value.String())
}

func passwordSpecialValidator(field string, value reflect.Value, params []string) Rule {
	if value.Kind() != reflect.String {
		return Rule{Check: func() bool { return true }}
	}
	return PasswordSpecialChar(field, value.String())
}

func notCommonPasswordValidator(field string, value reflect.Value, params []string) Rule {
	if value.Kind() != reflect.String {
		return Rule{Check: func() bool { return true }}
	}
	return NotCommonPassword(field, value.String())
}

// Pattern/Character validators

func asciiValidator(field string, value reflect.Value, params []string) Rule {
	if value.Kind() != reflect.String {
		return Rule{Check: func() bool { return true }}
	}
	return ASCIIOnly(field, value.String())
}

func noWhitespaceValidator(field string, value reflect.Value, params []string) Rule {
	if value.Kind() != reflect.String {
		return Rule{Check: func() bool { return true }}
	}
	return NoWhitespace(field, value.String())
}

func noControlCharsValidator(field string, value reflect.Value, params []string) Rule {
	if value.Kind() != reflect.String {
		return Rule{Check: func() bool { return true }}
	}
	return NoControlChars(field, value.String())
}

func printableValidator(field string, value reflect.Value, params []string) Rule {
	if value.Kind() != reflect.String {
		return Rule{Check: func() bool { return true }}
	}
	return PrintableChars(field, value.String())
}

func noSpecialCharsValidator(field string, value reflect.Value, params []string) Rule {
	if value.Kind() != reflect.String {
		return Rule{Check: func() bool { return true }}
	}
	return NoSpecialChars(field, value.String())
}

func containsUppercaseValidator(field string, value reflect.Value, params []string) Rule {
	if value.Kind() != reflect.String {
		return Rule{Check: func() bool { return true }}
	}
	return ContainsUppercase(field, value.String())
}

func containsLowercaseValidator(field string, value reflect.Value, params []string) Rule {
	if value.Kind() != reflect.String {
		return Rule{Check: func() bool { return true }}
	}
	return ContainsLowercase(field, value.String())
}

func containsDigitValidator(field string, value reflect.Value, params []string) Rule {
	if value.Kind() != reflect.String {
		return Rule{Check: func() bool { return true }}
	}
	return ContainsDigit(field, value.String())
}

func otpValidator(field string, value reflect.Value, params []string) Rule {
	if value.Kind() != reflect.String {
		return Rule{Check: func() bool { return true }}
	}
	length := 6
	if len(params) >= 1 {
		if l, err := strconv.Atoi(params[0]); err == nil {
			length = l
		}
	}
	return ValidOTP(field, value.String(), length)
}
