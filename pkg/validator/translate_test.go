package validator_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/pkg/validator"
)

func TestValidationErrors_Translate(t *testing.T) {
	t.Parallel()

	mockTranslate := func(key string, values map[string]any) string {
		translations := map[string]string{
			"validation.required":   "The {{field}} field is required.",
			"validation.min_length": "The {{field}} must be at least {{min}} characters long.",
		}
		tmpl := translations[key]
		if tmpl == "" {
			return key
		}
		result := tmpl
		for k, v := range values {
			token := "{{" + k + "}}"
			result = strings.ReplaceAll(result, token, formatValue(v))
		}
		return result
	}

	t.Run("translates messages in-place", func(t *testing.T) {
		t.Parallel()
		errs := validator.ValidationErrors{
			{
				Field:             "email",
				Message:           "is required",
				TranslationKey:    "validation.required",
				TranslationValues: map[string]any{"field": "email"},
			},
			{
				Field:             "password",
				Message:           "too short",
				TranslationKey:    "validation.min_length",
				TranslationValues: map[string]any{"field": "password", "min": 8},
			},
		}

		errs.Translate(mockTranslate)

		assert.Equal(t, "The email field is required.", errs[0].Message)
		assert.Equal(t, "The password must be at least 8 characters long.", errs[1].Message)
	})

	t.Run("nil fn is no-op", func(t *testing.T) {
		t.Parallel()
		errs := validator.ValidationErrors{
			{
				Field:          "email",
				Message:        "is required",
				TranslationKey: "validation.required",
			},
		}

		errs.Translate(nil)

		assert.Equal(t, "is required", errs[0].Message)
	})

	t.Run("empty errors is no-op", func(t *testing.T) {
		t.Parallel()
		var errs validator.ValidationErrors
		errs.Translate(mockTranslate) // should not panic
		assert.Empty(t, errs)
	})

	t.Run("skips errors with empty TranslationKey", func(t *testing.T) {
		t.Parallel()
		errs := validator.ValidationErrors{
			{
				Field:   "name",
				Message: "original message",
			},
			{
				Field:             "email",
				Message:           "is required",
				TranslationKey:    "validation.required",
				TranslationValues: map[string]any{"field": "email"},
			},
		}

		errs.Translate(mockTranslate)

		assert.Equal(t, "original message", errs[0].Message)
		assert.Equal(t, "The email field is required.", errs[1].Message)
	})

	t.Run("preserves Field, TranslationKey, and TranslationValues", func(t *testing.T) {
		t.Parallel()
		errs := validator.ValidationErrors{
			{
				Field:             "email",
				Message:           "is required",
				TranslationKey:    "validation.required",
				TranslationValues: map[string]any{"field": "email"},
			},
		}

		errs.Translate(mockTranslate)

		assert.Equal(t, "email", errs[0].Field)
		assert.Equal(t, "validation.required", errs[0].TranslationKey)
		assert.Equal(t, map[string]any{"field": "email"}, errs[0].TranslationValues)
	})

	t.Run("end-to-end with Apply", func(t *testing.T) {
		t.Parallel()
		err := validator.Apply(
			validator.RequiredString("email", ""),
			validator.MinLenString("password", "abc", 8),
		)
		require.Error(t, err)

		ve := validator.ExtractValidationErrors(err)
		require.NotNil(t, ve)

		ve.Translate(mockTranslate)

		assert.Equal(t, "The email field is required.", ve[0].Message)
		assert.Equal(t, "The password must be at least 8 characters long.", ve[1].Message)
	})
}

func formatValue(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case int:
		return intToStr(val)
	default:
		return "unknown"
	}
}

func intToStr(i int) string {
	if i == 0 {
		return "0"
	}
	negative := i < 0
	if negative {
		i = -i
	}
	digits := ""
	for i > 0 {
		digit := i % 10
		digits = string(rune('0'+digit)) + digits
		i /= 10
	}
	if negative {
		digits = "-" + digits
	}
	return digits
}
